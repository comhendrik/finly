package backend

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const passwordIterations = 310000
const sessionCookie = "finly_session"
const sessionLifetime = 12 * time.Hour
const maxAccountImportBody = 25 << 20

func pbkdf2SHA256(password, salt []byte, iterations, length int) []byte {
	result := make([]byte, 0, length)
	for block := uint32(1); len(result) < length; block++ {
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
	mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		acc := append([]byte(nil), u...)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range acc { acc[j] ^= u[j] }
		}
		result = append(result, acc...)
	}
	return result[:length]
}

func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, ":")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" { return false }
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < passwordIterations || iterations > 1000000 { return false }
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[2])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[3])
	if err1 != nil || err2 != nil || len(salt) < 16 || len(want) != 32 { return false }
	got := pbkdf2SHA256([]byte(password), salt, iterations, len(want))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func validPasswordHash(encoded string) bool {
	parts := strings.Split(encoded, ":")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" { return false }
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < passwordIterations || iterations > 1000000 { return false }
	salt, saltErr := base64.RawStdEncoding.DecodeString(parts[2])
	hash, hashErr := base64.RawStdEncoding.DecodeString(parts[3])
	return saltErr == nil && hashErr == nil && len(salt) >= 16 && len(hash) == 32
}

func (a *App) signSession(username string, expires int64) string {
	a.authMu.RLock()
	secret := append([]byte(nil), a.sessionSecret...)
	a.authMu.RUnlock()
	encodedUsername := base64.RawURLEncoding.EncodeToString([]byte(username))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%s|%d", encodedUsername, expires)))
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *App) signJWT(username string, now time.Time) (string, string, error) {
	a.authMu.RLock()
	secret := append([]byte(nil), a.jwtSecret...)
	a.authMu.RUnlock()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	jtiBytes := make([]byte, 16)
	if _, err := rand.Read(jtiBytes); err != nil { return "", "", err }
	claims, err := json.Marshal(map[string]any{
		"sub": username,
		"scope": "expenses:create",
		"iat": now.Unix(),
		"exp": now.AddDate(1, 0, 0).Unix(),
		"jti": base64.RawURLEncoding.EncodeToString(jtiBytes),
	})
	if err != nil { return "", "", err }
	payload := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	token := payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	digest := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(digest[:]), nil
}

func (a *App) validAPIToken(token string) bool {
	if len(token) > 8192 { return false }
	parts := strings.Split(token, ".")
	if len(parts) != 3 { return false }
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil { return false }
	var header struct { Algorithm string `json:"alg"`; Type string `json:"typ"` }
	if json.Unmarshal(headerJSON, &header) != nil || header.Algorithm != "HS256" || header.Type != "JWT" { return false }
	a.authMu.RLock()
	secret := append([]byte(nil), a.jwtSecret...)
	wantHash := a.apiKeyHash
	username := a.authUsername
	a.authMu.RUnlock()
	if wantHash == "" { return false }
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil { return false }
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(sig, mac.Sum(nil)) { return false }
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil { return false }
	var claims struct { Subject string `json:"sub"`; Scope string `json:"scope"`; Expires int64 `json:"exp"`; IssuedAt int64 `json:"iat"`; ID string `json:"jti"` }
	if json.Unmarshal(claimsJSON, &claims) != nil || claims.Subject != username || claims.Scope != "expenses:create" || claims.ID == "" || claims.IssuedAt <= 0 || claims.IssuedAt > time.Now().Add(time.Minute).Unix() || claims.Expires <= claims.IssuedAt || time.Now().Unix() >= claims.Expires { return false }
	digest := sha256.Sum256([]byte(token))
	gotHash := hex.EncodeToString(digest[:])
	return subtle.ConstantTimeCompare([]byte(gotHash), []byte(wantHash)) == 1
}

func (a *App) validSession(value string) bool {
	a.authMu.RLock()
	usernameExpected := a.authUsername
	secret := append([]byte(nil), a.sessionSecret...)
	a.authMu.RUnlock()
	parts := strings.Split(value, ".")
	if len(parts) != 2 { return false }
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil { return false }
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(parts[0]))
	if !hmac.Equal(sig, mac.Sum(nil)) { return false }
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil { return false }
	fields := strings.Split(string(payload), "|")
	if len(fields) != 2 { return false }
	username, err := base64.RawURLEncoding.DecodeString(fields[0])
	if err != nil || string(username) != usernameExpected { return false }
	expires, err := strconv.ParseInt(fields[1], 10, 64)
	return err == nil && time.Now().Unix() < expires
}

func (a *App) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "same-origin")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		isAPI := strings.HasPrefix(c.Request.URL.Path, "/api/")
		if c.Request.Method == http.MethodPost && !isAPI && !sameOrigin(c.Request) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		if c.Request.Method == http.MethodPost {
			maxBody := int64(1 << 20)
			if c.Request.URL.Path == "/account/import" { maxBody = maxAccountImportBody }
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBody)
		}
		path := c.Request.URL.Path
		if path == "/health" {
			c.Next()
			return
		}
		if strings.HasPrefix(path, "/static/") {
			c.Next()
			return
		}
		if path == "/signup" || path == "/setup" || path == "/login" { c.Next(); return }
		a.tenantRequestMu.Lock()
		defer a.tenantRequestMu.Unlock()
		if isAPI {
			username := apiTokenUsername(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
			if username == "" || a.loadTenant(username) != nil {
				c.Header("WWW-Authenticate", `Bearer realm="finly-api"`)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "valid bearer token required"})
				return
			}
			c.Next()
			return
		}
		cookie, err := c.Request.Cookie(sessionCookie)
		username := ""
		if err == nil { username = sessionUsername(cookie.Value) }
		if username != "" && a.loadTenant(username) == nil && a.validSession(cookie.Value) {
			c.Next()
			return
		}
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
			c.Redirect(http.StatusSeeOther, "/login")
		} else {
			c.AbortWithStatus(http.StatusUnauthorized)
		}
		c.Abort()
	}
}

func sessionUsername(value string) string {
	parts := strings.Split(value, ".")
	if len(parts) != 2 { return "" }
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil { return "" }
	fields := strings.Split(string(payload), "|")
	if len(fields) != 2 { return "" }
	username, err := base64.RawURLEncoding.DecodeString(fields[0])
	if err != nil { return "" }
	return string(username)
}

func apiTokenUsername(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 { return "" }
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil { return "" }
	var claims struct { Subject string `json:"sub"` }
	if json.Unmarshal(claimsJSON, &claims) != nil { return "" }
	return claims.Subject
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" { origin = r.Header.Get("Referer") }
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" { return false }
	requestHost := r.Host
	if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" { requestHost = forwarded }
	return strings.EqualFold(parsed.Host, requestHost) && (parsed.Scheme == "https" || parsed.Scheme == "http")
}

var loginAttempts = struct { sync.Mutex; attempts map[string][]time.Time }{attempts: make(map[string][]time.Time)}

func allowLogin(ip string) bool {
	now := time.Now()
	loginAttempts.Lock()
	defer loginAttempts.Unlock()
	cutoff := now.Add(-15 * time.Minute)
	_, existed := loginAttempts.attempts[ip]
	items := loginAttempts.attempts[ip][:0]
	for _, stamp := range loginAttempts.attempts[ip] { if stamp.After(cutoff) { items = append(items, stamp) } }
	if !existed && len(loginAttempts.attempts) >= 10000 {
		for address, stamps := range loginAttempts.attempts {
			if len(stamps) == 0 || stamps[len(stamps)-1].Before(cutoff) { delete(loginAttempts.attempts, address) }
		}
		if len(loginAttempts.attempts) >= 10000 { return false }
	}
	loginAttempts.attempts[ip] = items
	return len(items) < 10
}

func recordLogin(ip string) {
	loginAttempts.Lock()
	loginAttempts.attempts[ip] = append(loginAttempts.attempts[ip], time.Now())
	loginAttempts.Unlock()
}

func clientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil { return ip }
	return r.RemoteAddr
}

func (a *App) loginPage(c *gin.Context) { c.HTML(http.StatusOK, "login.html", nil) }

func (a *App) signupPage(c *gin.Context) {
	if c.Query("deleted") == "1" { c.HTML(http.StatusOK, "signup.html", gin.H{"Notice": "Dein Konto und die zugehörigen Daten wurden gelöscht."}); return }
	c.HTML(http.StatusOK, "signup.html", nil)
}

func (a *App) setupPage(c *gin.Context) { c.Redirect(http.StatusSeeOther, "/signup") }

func (a *App) setup(c *gin.Context) {
	a.signup(c)
}

func (a *App) signup(c *gin.Context) {
	a.tenantRequestMu.Lock()
	defer a.tenantRequestMu.Unlock()
	ip := clientIP(c.Request)
	if !allowLogin(ip) { c.Header("Retry-After", "900"); c.String(http.StatusTooManyRequests, "Zu viele Versuche. Bitte später erneut versuchen."); return }
	recordLogin(ip)
	c.Header("Cache-Control", "no-store")
	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")
	confirm := c.PostForm("password_confirm")
	if len(username) < 3 || len(username) > 64 || len(password) < 12 || len(password) > 1024 || password != confirm {
		c.HTML(http.StatusBadRequest, "signup.html", gin.H{"Error": "Bitte einen Benutzernamen mit 3–64 Zeichen und ein übereinstimmendes Passwort mit mindestens 12 Zeichen eingeben."})
		return
	}
	salt := make([]byte, 16)
	secret := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil { c.String(http.StatusInternalServerError, "Konto konnte nicht erstellt werden."); return }
	if _, err := rand.Read(secret); err != nil { c.String(http.StatusInternalServerError, "Konto konnte nicht erstellt werden."); return }
	jwtSecret := make([]byte, 32)
	if _, err := rand.Read(jwtSecret); err != nil { c.String(http.StatusInternalServerError, "Konto konnte nicht erstellt werden."); return }
	hash := pbkdf2SHA256([]byte(password), salt, passwordIterations, 32)
	config := AuthConfig{
		Username: username,
		PasswordHash: fmt.Sprintf("pbkdf2-sha256:%d:%s:%s", passwordIterations, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)),
		SessionKey: base64.RawStdEncoding.EncodeToString(secret),
		JWTSecret: base64.RawStdEncoding.EncodeToString(jwtSecret),
	}

	state := Store{Months: make(map[string]MonthData), FixedCosts: []FixedCost{}, Auth: &config}
	if a.legacyAvailable {
		state = a.legacyData
		state.Auth = &config
		if state.Months == nil { state.Months = make(map[string]MonthData) }
		if state.FixedCosts == nil { state.FixedCosts = []FixedCost{} }
	}
	contents, err := json.Marshal(state)
	if err != nil { c.String(http.StatusInternalServerError, "Konto konnte nicht erstellt werden."); return }
	tx, err := a.db.Begin()
	if err != nil { c.String(http.StatusInternalServerError, "Konto konnte nicht erstellt werden."); return }
	defer tx.Rollback()
	key := strings.ToLower(username)
	result, err := tx.Exec(`INSERT INTO finly_users (username_key, username, password_hash) VALUES ($1, $2, $3) ON CONFLICT (username_key) DO NOTHING`, key, username, config.PasswordHash)
	if err != nil { c.String(http.StatusInternalServerError, "Konto konnte nicht erstellt werden."); return }
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		c.HTML(http.StatusConflict, "signup.html", gin.H{"Error": "Dieser Benutzername ist bereits vergeben."})
		return
	}
	if _, err = tx.Exec(`INSERT INTO finly_tenant_state (username_key, payload) VALUES ($1, $2::jsonb)`, key, string(contents)); err != nil {
		c.String(http.StatusInternalServerError, "Konto konnte nicht erstellt werden.")
		return
	}
	if a.legacyAvailable {
		if _, err = tx.Exec(`DELETE FROM finly_state WHERE id = 1`); err != nil {
			c.String(http.StatusInternalServerError, "Konto konnte nicht erstellt werden.")
			return
		}
	}
	if err = tx.Commit(); err != nil { c.String(http.StatusInternalServerError, "Konto konnte nicht erstellt werden."); return }
	if err := a.loadTenant(username); err != nil { c.String(http.StatusInternalServerError, "Konto konnte nicht geladen werden."); return }
	a.legacyAvailable = false
	a.legacyData = Store{}
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(sessionCookie, a.signSession(username, time.Now().Add(sessionLifetime).Unix()), int(sessionLifetime.Seconds()), "/", "", a.secureCookie, true)
	c.Redirect(http.StatusSeeOther, "/")
}

func (a *App) login(c *gin.Context) {
	a.tenantRequestMu.Lock()
	defer a.tenantRequestMu.Unlock()
	ip := clientIP(c.Request)
	if !allowLogin(ip) { c.Header("Retry-After", "900"); c.String(http.StatusTooManyRequests, "Zu viele Versuche. Bitte später erneut versuchen."); return }
	username := c.PostForm("username")
	password := c.PostForm("password")
	if len(password) > 1024 { password = "invalid-password-input" }
	loadErr := a.loadTenant(strings.TrimSpace(username))
	usernameExpected, passwordHash := "", ""
	if loadErr == nil {
		a.authMu.RLock()
		usernameExpected, passwordHash = a.authUsername, a.authPasswordHash
		a.authMu.RUnlock()
	}
	passwordOK := verifyPassword(password, passwordHash)
	usernameOK := loadErr == nil && subtle.ConstantTimeCompare([]byte(strings.ToLower(strings.TrimSpace(username))), []byte(strings.ToLower(usernameExpected))) == 1
	if !usernameOK || !passwordOK {
		recordLogin(ip)
		c.HTML(http.StatusUnauthorized, "login.html", gin.H{"Error": "Anmeldung fehlgeschlagen."})
		return
	}
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(sessionCookie, a.signSession(usernameExpected, time.Now().Add(sessionLifetime).Unix()), int(sessionLifetime.Seconds()), "/", "", a.secureCookie, true)
	c.Redirect(http.StatusSeeOther, "/")
}

func (a *App) logout(c *gin.Context) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(sessionCookie, "", -1, "/", "", a.secureCookie, true)
	c.Redirect(http.StatusSeeOther, "/login")
}

func (a *App) apiAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") || !a.validAPIToken(strings.TrimPrefix(header, "Bearer ")) {
			c.Header("WWW-Authenticate", `Bearer realm="finly-api"`)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "valid bearer token required"})
			return
		}
		c.Next()
	}
}
