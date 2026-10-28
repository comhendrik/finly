package backend

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	exportservice "finly/backend/services"
	"github.com/gin-gonic/gin"
)

func (a *App) changePassword(c *gin.Context) {
	a.authMu.RLock()
	username, currentHash := a.authUsername, a.authPasswordHash
	a.authMu.RUnlock()
	current := c.PostForm("current_password")
	next := c.PostForm("new_password")
	if len(current) > 1024 || !verifyPassword(current, currentHash) {
		redirectAccountError(c, "wrong-current-password")
		return
	}
	if len(next) < 12 || len(next) > 1024 || next != c.PostForm("new_password_confirm") {
		redirectAccountError(c, "invalid-new-password")
		return
	}

	salt := make([]byte, 16)
	sessionKey := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil { c.String(http.StatusInternalServerError, "Passwort konnte nicht geändert werden."); return }
	if _, err := rand.Read(sessionKey); err != nil { c.String(http.StatusInternalServerError, "Passwort konnte nicht geändert werden."); return }
	passwordHash := fmt.Sprintf("pbkdf2-sha256:%d:%s:%s", passwordIterations, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(pbkdf2SHA256([]byte(next), salt, passwordIterations, 32)))

	a.mu.Lock()
	oldHash, oldSessionKey := a.data.Auth.PasswordHash, a.data.Auth.SessionKey
	a.data.Auth.PasswordHash = passwordHash
	a.data.Auth.SessionKey = base64.RawStdEncoding.EncodeToString(sessionKey)
	contents, err := json.Marshal(a.data)
	if err != nil {
		a.data.Auth.PasswordHash, a.data.Auth.SessionKey = oldHash, oldSessionKey
		a.mu.Unlock()
		c.String(http.StatusInternalServerError, "Passwort konnte nicht geändert werden.")
		return
	}
	tx, err := a.db.Begin()
	if err == nil {
		_, err = tx.Exec(`UPDATE finly_users SET password_hash = $1 WHERE username_key = $2`, passwordHash, a.currentTenant)
		if err == nil { _, err = tx.Exec(`UPDATE finly_tenant_state SET payload = $1::jsonb WHERE username_key = $2`, string(contents), a.currentTenant) }
		if err == nil { err = tx.Commit() } else { _ = tx.Rollback() }
	}
	if err != nil {
		a.data.Auth.PasswordHash, a.data.Auth.SessionKey = oldHash, oldSessionKey
		a.mu.Unlock()
		c.String(http.StatusInternalServerError, "Passwort konnte nicht geändert werden.")
		return
	}
	a.authMu.Lock()
	a.authPasswordHash, a.sessionSecret = passwordHash, sessionKey
	a.authMu.Unlock()
	a.mu.Unlock()
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(sessionCookie, a.signSession(username, time.Now().Add(sessionLifetime).Unix()), int(sessionLifetime.Seconds()), "/", "", a.secureCookie, true)
	c.Redirect(http.StatusSeeOther, "/?tab=account&account_notice=password-changed")
}

func (a *App) deleteAccount(c *gin.Context) {
	a.authMu.RLock()
	username, currentHash := a.authUsername, a.authPasswordHash
	a.authMu.RUnlock()
	password := c.PostForm("password")
	confirmed := strings.EqualFold(strings.TrimSpace(c.PostForm("confirm_username")), username)
	if len(password) > 1024 || !verifyPassword(password, currentHash) || !confirmed {
		redirectAccountError(c, "confirm-delete")
		return
	}
	tx, err := a.db.Begin()
	if err != nil { c.String(http.StatusInternalServerError, "Konto konnte nicht gelöscht werden."); return }
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM finly_users WHERE username_key = $1`, a.currentTenant); err != nil {
		c.String(http.StatusInternalServerError, "Konto konnte nicht gelöscht werden.")
		return
	}
	if _, err := tx.Exec(`DELETE FROM finly_state WHERE id = 1 AND lower(COALESCE(payload->'auth'->>'username', '')) = lower($1)`, username); err != nil {
		c.String(http.StatusInternalServerError, "Konto konnte nicht gelöscht werden.")
		return
	}
	if err := tx.Commit(); err != nil { c.String(http.StatusInternalServerError, "Konto konnte nicht gelöscht werden."); return }
	a.currentTenant = ""
	a.data = Store{Months: make(map[string]MonthData)}
	a.authMu.Lock()
	a.authUsername, a.authPasswordHash = "", ""
	a.sessionSecret, a.jwtSecret = nil, nil
	a.apiKeyHash, a.apiKeyCreatedAt = "", ""
	a.authMu.Unlock()
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(sessionCookie, "", -1, "/", "", a.secureCookie, true)
	c.Redirect(http.StatusSeeOther, "/signup?deleted=1")
}

func (a *App) exportAccountData(c *gin.Context) {
	a.mu.Lock()
	export, err := exportservice.ExportData(a.data, c.Query("from_month"), c.Query("to_month"), a.defaultsForMonthLocked)
	a.mu.Unlock()
	if err != nil {
		redirectAccountError(c, "invalid-month-range")
		return
	}
	contents, err := json.MarshalIndent(export, "", "  ")
	if err != nil { c.String(http.StatusInternalServerError, "Export konnte nicht erstellt werden."); return }
	filename := fmt.Sprintf("finly-export-%s-to-%s.json", export.FromMonth, export.ToMonth)
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/json; charset=utf-8", contents)
}

func (a *App) importAccountData(c *gin.Context) {
	if c.PostForm("replace_months") != "yes" { redirectAccountError(c, "confirm-import"); return }
	file, _, err := c.Request.FormFile("file")
	if err != nil { redirectAccountError(c, "invalid-import-file"); return }
	defer file.Close()
	export, err := exportservice.DecodeDataExport(io.LimitReader(file, maxAccountImportBody))
	if err != nil { redirectAccountError(c, "invalid-import-file"); return }

	a.mu.Lock()
	previous := make(map[string]MonthData, len(export.Months))
	existed := make(map[string]bool, len(export.Months))
	for key := range export.Months { previous[key], existed[key] = a.data.Months[key] }
	exportservice.ApplyDataExport(&a.data, export)
	if err := a.saveLocked(); err != nil {
		for key := range export.Months {
			if existed[key] { a.data.Months[key] = previous[key] } else { delete(a.data.Months, key) }
		}
		a.mu.Unlock()
		c.String(http.StatusInternalServerError, "Daten konnten nicht importiert werden.")
		return
	}
	a.mu.Unlock()
	c.Redirect(http.StatusSeeOther, "/?tab=account&account_notice=imported")
}

func redirectAccountError(c *gin.Context, code string) {
	c.Redirect(http.StatusSeeOther, "/?tab=account&account_error="+code)
}

