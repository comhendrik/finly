package backend

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
)

func (a *App) addBudgetExpense(c *gin.Context) {
	name := strings.TrimSpace(c.PostForm("name"))
	amount := parseAmount(c.PostForm("amount"))
	category := strings.TrimSpace(c.PostForm("category"))
	if name == "" || amount <= 0 {
		a.redirectAfterSave(c)
		return
	}
	_, _, err := a.CreateExpense(CreateExpenseInput{Month: c.PostForm("month"), BudgetID: c.Param("id"), Name: name, Amount: amount, Category: category})
	if err != nil && !errors.Is(err, errBudgetNotFound) && !errors.Is(err, errInvalidExpense) { c.String(500, "Ausgabe konnte nicht gespeichert werden."); return }
	a.redirectAfterSave(c)
}

func (a *App) deleteBudgetExpense(c *gin.Context) {
	a.mu.Lock()
	monthKey, month := a.monthDataLocked(c.PostForm("month"))

	for i := range month.Budgets {
		budget := &month.Budgets[i]
		if budget.ID != c.Param("id") {
			continue
		}
		budget.Expenses = withoutExpense(budget.Expenses, c.Param("expenseID"))
		break
	}

	a.saveMonthAndRedirect(c, monthKey, month)
}

func (a *App) addStandardExpense(c *gin.Context) {
	name := strings.TrimSpace(c.PostForm("name"))
	amount := parseAmount(c.PostForm("amount"))
	category := strings.TrimSpace(c.PostForm("category"))
	if category == "" {
		category = "Allgemein"
	}
	if name == "" || amount <= 0 {
		a.redirectAfterSave(c)
		return
	}

	_, _, err := a.CreateExpense(CreateExpenseInput{Month: c.PostForm("month"), Name: name, Amount: amount, Category: category})
	if err != nil && !errors.Is(err, errInvalidExpense) { c.String(500, "Ausgabe konnte nicht gespeichert werden."); return }
	a.redirectAfterSave(c)
}

func (a *App) deleteStandardExpense(c *gin.Context) {
	a.mu.Lock()
	monthKey, month := a.monthDataLocked(c.PostForm("month"))
	month.StandardExpenses = withoutExpense(month.StandardExpenses, c.Param("id"))

	a.saveMonthAndRedirect(c, monthKey, month)
}

func withoutExpense(expenses []Expense, id string) []Expense {
	remaining := make([]Expense, 0, len(expenses))
	for _, expense := range expenses {
		if expense.ID != id {
			remaining = append(remaining, expense)
		}
	}
	return remaining
}

// saveMonthAndRedirect expects the caller to hold a.mu.
func (a *App) saveMonthAndRedirect(c *gin.Context, key string, month MonthData) {
	a.data.Months[key] = month
	err := a.saveLocked()
	a.mu.Unlock()
	if err != nil {
		c.String(500, err.Error())
		return
	}
	a.redirectAfterSave(c)
}
