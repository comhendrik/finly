package backend

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// load initializes tenant tables and migrates an older PostgreSQL singleton
// state when present. Data files are only imported explicitly by the user.
func (a *App) load() error {
	if _, err := a.db.Exec(`CREATE TABLE IF NOT EXISTS finly_state (id SMALLINT PRIMARY KEY CHECK (id = 1), payload JSONB NOT NULL)`); err != nil {
		return fmt.Errorf("create state table: %w", err)
	}
	if _, err := a.db.Exec(`CREATE TABLE IF NOT EXISTS finly_users (username_key TEXT PRIMARY KEY, username TEXT NOT NULL, password_hash TEXT NOT NULL)`); err != nil { return fmt.Errorf("create users table: %w", err) }
	if _, err := a.db.Exec(`CREATE TABLE IF NOT EXISTS finly_tenant_state (username_key TEXT PRIMARY KEY REFERENCES finly_users(username_key) ON DELETE CASCADE, payload JSONB NOT NULL)`); err != nil { return fmt.Errorf("create tenant state table: %w", err) }
	var contents []byte
	err := a.db.QueryRow(`SELECT payload FROM finly_state WHERE id = 1`).Scan(&contents)
	if errors.Is(err, sql.ErrNoRows) { return nil }
	if err != nil {
		return fmt.Errorf("read PostgreSQL state: %w", err)
	}
	var legacy Store
	if err := json.Unmarshal(contents, &legacy); err != nil { return fmt.Errorf("decode stored data: %w", err) }
	if legacy.Months == nil { legacy.Months = make(map[string]MonthData) }
	if legacy.FixedCosts == nil { legacy.FixedCosts = []FixedCost{} }
	if legacy.Auth == nil {
		var hasUsers bool
		if err := a.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM finly_users)`).Scan(&hasUsers); err != nil { return err }
		if hasUsers { return nil }
		a.legacyData = legacy
		a.legacyAvailable = len(legacy.Months) > 0 || len(legacy.FixedCosts) > 0 || legacy.Income != 0
		return nil
	}
	auth := legacy.Auth
	if strings.TrimSpace(auth.Username) == "" || !validPasswordHash(auth.PasswordHash) { return errors.New("invalid authentication data in stored state") }
	key := strings.ToLower(auth.Username)
	tx, err := a.db.Begin()
	if err != nil { return err }
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO finly_users (username_key, username, password_hash) VALUES ($1, $2, $3) ON CONFLICT (username_key) DO NOTHING`, key, auth.Username, auth.PasswordHash); err != nil { return err }
	if _, err := tx.Exec(`INSERT INTO finly_tenant_state (username_key, payload) VALUES ($1, $2::jsonb) ON CONFLICT (username_key) DO NOTHING`, key, string(contents)); err != nil { return err }
	if err := tx.Commit(); err != nil { return err }
	a.data = Store{Months: make(map[string]MonthData), FixedCosts: []FixedCost{}}
	return nil
}

func (a *App) loadTenant(username string) error {
	var contents []byte
	var canonical string
	err := a.db.QueryRow(`SELECT u.username, s.payload FROM finly_users u JOIN finly_tenant_state s USING (username_key) WHERE u.username_key = lower($1)`, username).Scan(&canonical, &contents)
	if err != nil { return err }
	var state Store
	if err := json.Unmarshal(contents, &state); err != nil { return fmt.Errorf("decode tenant state: %w", err) }
	if state.Auth == nil || !validPasswordHash(state.Auth.PasswordHash) { return errors.New("invalid tenant authentication data") }
	if state.Months == nil { state.Months = make(map[string]MonthData) }
	activeMonth := normalizeActiveMonth(state.ActiveMonth)
	needsMigration := state.ActiveMonth != activeMonth
	state.ActiveMonth = activeMonth
	state.APIMappings, needsMigration = normalizeAPIMappings(state.APIMappings, needsMigration)
	if state.FixedCosts == nil { state.FixedCosts = []FixedCost{} }
	key, err := base64.RawStdEncoding.DecodeString(state.Auth.SessionKey)
	if err != nil || len(key) != 32 { return errors.New("invalid session key") }
	jwtSecret, jwtErr := base64.RawStdEncoding.DecodeString(state.Auth.JWTSecret)
	if state.Auth.JWTSecret == "" {
		jwtSecret = make([]byte, 32)
		if _, err := rand.Read(jwtSecret); err != nil { return err }
		state.Auth.JWTSecret = base64.RawStdEncoding.EncodeToString(jwtSecret)
	} else if jwtErr != nil || len(jwtSecret) != 32 { return errors.New("invalid API token key") }
	a.data, a.currentTenant = state, strings.ToLower(canonical)
	a.authMu.Lock()
	a.authUsername, a.authPasswordHash = canonical, state.Auth.PasswordHash
	a.sessionSecret, a.jwtSecret = key, jwtSecret
	a.apiKeyHash, a.apiKeyCreatedAt = state.Auth.APIKeyHash, state.Auth.APIKeyCreatedAt
	a.authMu.Unlock()
	if state.Auth.JWTSecret != "" && jwtErr == nil && !needsMigration { return nil }
	return a.saveLocked()
}

func normalizeActiveMonth(raw string) string {
	if month, err := time.Parse("2006-01", raw); err == nil && month.Format("2006-01") == raw {
		return raw
	}
	return time.Now().Format("2006-01")
}

func normalizeAPIMappings(raw []APIMapping, changed bool) ([]APIMapping, bool) {
	if raw == nil {
		return raw, changed
	}
	mappings := make([]APIMapping, 0, len(raw))
	categoryIndexes := make(map[string]int)
	seenNames := make(map[string]bool)
	for _, mapping := range raw {
		names := append([]string{}, mapping.Names...)
		if mapping.LegacyName != "" {
			names = append(names, mapping.LegacyName)
			changed = true
		}
		category := strings.TrimSpace(mapping.Category)
		if category != mapping.Category {
			changed = true
		}
		if category == "" {
			changed = true
			continue
		}
		key := strings.ToLower(category)
		index, exists := categoryIndexes[key]
		if exists {
			changed = true
		} else {
			mapping.Category = category
			mapping.Names = []string{}
			mapping.LegacyName = ""
			index = len(mappings)
			categoryIndexes[key] = index
			mappings = append(mappings, mapping)
		}
		for _, name := range names {
		trimmedName := strings.TrimSpace(name)
		if trimmedName != name {
			changed = true
		}
		name = trimmedName
			nameKey := strings.ToLower(name)
			if name == "" || seenNames[nameKey] {
				changed = true
				continue
			}
			seenNames[nameKey] = true
			mappings[index].Names = append(mappings[index].Names, name)
		}
	}
	return mappings, changed
}

// saveLocked persists the application state as a single JSONB row. Callers
// serialize writes with a.mu.
func (a *App) saveLocked() error {
	if a.currentTenant == "" { return errors.New("no tenant is selected") }
	contents, err := json.Marshal(a.data)
	if err != nil { return err }
	_, err = a.db.Exec(`UPDATE finly_tenant_state SET payload = $1::jsonb WHERE username_key = $2`, string(contents), a.currentTenant)
	if err != nil { return fmt.Errorf("write PostgreSQL state: %w", err) }
	return nil
}

// defaultsForMonthLocked uses the nearest saved month as a starting point.
// The caller must hold a.mu.
func (a *App) defaultsForMonthLocked(monthKey string) (float64, []FixedCost) {
	target := parseMonth(monthKey)
	var previous, following MonthData
	var previousDate, followingDate time.Time
	var hasPrevious, hasFollowing bool
	for key, month := range a.data.Months {
		if !month.Initialized { continue }
		date, err := time.Parse("2006-01", key)
		if err != nil { continue }
		if !date.After(target) && (!hasPrevious || date.After(previousDate)) { previous, previousDate, hasPrevious = month, date, true }
		if date.After(target) && (!hasFollowing || date.Before(followingDate)) { following, followingDate, hasFollowing = month, date, true }
	}
	if hasPrevious { return previous.Income, append([]FixedCost{}, previous.FixedCosts...) }
	if hasFollowing { return following.Income, append([]FixedCost{}, following.FixedCosts...) }
	return a.data.Income, append([]FixedCost{}, a.data.FixedCosts...)
}

// monthDataLocked returns a writable month snapshot. The caller must hold a.mu.
func (a *App) monthDataLocked(rawMonth string) (string, MonthData) {
	if strings.TrimSpace(rawMonth) == "" { rawMonth = a.data.ActiveMonth }
	key := parseMonth(rawMonth).Format("2006-01")
	month := a.data.Months[key]
	if !month.Initialized { month.Income, month.FixedCosts = a.defaultsForMonthLocked(key); month.Initialized = true }
	if month.Budgets == nil { month.Budgets = []Budget{} }
	if month.StandardExpenses == nil { month.StandardExpenses = []Expense{} }
	return key, month
}
