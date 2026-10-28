package backend

import (
	"encoding/json"
	"io"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type createExpenseRequest struct {
	Month    string  `json:"month"`
	BudgetID string  `json:"budget_id,omitempty"`
	Name     string  `json:"name"`
	Amount   string  `json:"amount"`
	Date     string  `json:"date,omitempty"`
	Category string  `json:"category,omitempty"`
}

func filterAmountText(raw string) string {
	var filtered strings.Builder
	filtered.Grow(len(raw))
	for _, char := range raw {
		if (char >= '0' && char <= '9') || char == ',' {
			filtered.WriteRune(char)
		}
	}
	return filtered.String()
}

func (a *App) apiCreateExpense(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var request createExpenseRequest
	if err := decoder.Decode(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request must contain one JSON object"})
		return
	}
	request.Amount = filterAmountText(request.Amount)
	amount := parseAmount(request.Amount)
	if strings.TrimSpace(request.Month) == "" || strings.TrimSpace(request.Name) == "" || amount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "month, name and a positive amount are required"})
		return
	}
	budgetID := strings.TrimSpace(request.BudgetID)
	if request.Date != "" {
		date, err := time.Parse("2006-01-02", request.Date)
		if err != nil || date.Format("2006-01-02") != request.Date {
			c.JSON(http.StatusBadRequest, gin.H{"error": "date must use YYYY-MM-DD"})
			return
		}
	}
	expense, month, err := a.CreateExpense(CreateExpenseInput{
		Month: request.Month, BudgetID: budgetID, Name: request.Name,
		Amount: amount, Date: request.Date, Category: request.Category,
	})
	if err != nil {
		if errors.Is(err, errBudgetNotFound) { c.JSON(http.StatusNotFound, gin.H{"error": "budget not found for this month"}); return }
		if errors.Is(err, errInvalidExpense) { c.JSON(http.StatusBadRequest, gin.H{"error": "invalid month or expense data"}); return }
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save expense"})
		return
	}
	kind := "standard"
	if budgetID != "" { kind = "budget" }
	c.JSON(http.StatusCreated, gin.H{"id": expense.ID, "month": month, "type": kind, "expense": expense})
}

func (a *App) createAPIKey(c *gin.Context) {
	a.authMu.RLock()
	passwordHash, username := a.authPasswordHash, a.authUsername
	a.authMu.RUnlock()
	password := c.PostForm("password")
	if len(password) > 1024 || !verifyPassword(password, passwordHash) {
		c.Header("Cache-Control", "no-store")
		c.HTML(http.StatusUnauthorized, "api_key_error.html", gin.H{"Error": "Passwort falsch. Der API-Key wurde nicht geändert."})
		return
	}
	token, hash, err := a.signJWT(username, time.Now())
	if err != nil { c.String(http.StatusInternalServerError, "API-Key konnte nicht erstellt werden."); return }
	createdAt := time.Now().UTC().Format(time.RFC3339)
	if err := a.storeAPIKey(hash, createdAt); err != nil { c.String(http.StatusInternalServerError, "API-Key konnte nicht gespeichert werden."); return }
	c.Header("Cache-Control", "no-store, private")
	c.Header("Pragma", "no-cache")
	c.Header("Referrer-Policy", "no-referrer")
	c.HTML(http.StatusOK, "api_key_created.html", gin.H{"Token": token, "ExpiresAt": time.Now().AddDate(1, 0, 0).UTC().Format(time.RFC3339)})
}

func (a *App) revokeAPIKey(c *gin.Context) {
	a.authMu.RLock()
	passwordHash := a.authPasswordHash
	a.authMu.RUnlock()
	password := c.PostForm("password")
	if len(password) > 1024 || !verifyPassword(password, passwordHash) {
		c.Header("Cache-Control", "no-store")
		c.HTML(http.StatusUnauthorized, "api_key_error.html", gin.H{"Error": "Passwort falsch. Der API-Key wurde nicht entfernt."})
		return
	}
	if err := a.storeAPIKey("", ""); err != nil { c.String(http.StatusInternalServerError, "API-Key konnte nicht entfernt werden."); return }
	c.Redirect(http.StatusSeeOther, "/?tab=settings&saved=API-Key+entfernt")
}

func (a *App) storeAPIKey(hash, createdAt string) error {
	a.mu.Lock()
	if a.data.Auth == nil { a.mu.Unlock(); return errors.New("account not configured") }
	oldHash, oldCreatedAt := a.data.Auth.APIKeyHash, a.data.Auth.APIKeyCreatedAt
	a.data.Auth.APIKeyHash, a.data.Auth.APIKeyCreatedAt = hash, createdAt
	if err := a.saveLocked(); err != nil {
		a.data.Auth.APIKeyHash, a.data.Auth.APIKeyCreatedAt = oldHash, oldCreatedAt
		a.mu.Unlock()
		return err
	}
	a.authMu.Lock()
	a.apiKeyHash, a.apiKeyCreatedAt = hash, createdAt
	a.authMu.Unlock()
	a.mu.Unlock()
	return nil
}
