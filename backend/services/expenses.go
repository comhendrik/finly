package services

import (
	"errors"
	"strings"
	"time"

	"finly/backend/domain"
)

var ErrBudgetNotFound = errors.New("budget not found")
var ErrInvalidExpense = errors.New("invalid expense")

type CreateExpenseInput struct {
	Month    string
	BudgetID string
	ApplyAPIMapping bool
	Name     string
	Amount   float64
	Date     string
	Category string
}

// CreateExpense applies the expense rules and updates the supplied tenant state.
func CreateExpense(state *domain.Store, input CreateExpenseInput, monthData func(string) (string, domain.MonthData), newID func() string) (domain.Expense, string, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.BudgetID = strings.TrimSpace(input.BudgetID)
	input.Category = strings.TrimSpace(input.Category)
	month, err := time.Parse("2006-01", input.Month)
	if err != nil || month.Format("2006-01") != input.Month || input.Name == "" || input.Amount <= 0 {
		return domain.Expense{}, "", ErrInvalidExpense
	}
	if input.Date == "" { input.Date = time.Now().Format("2006-01-02") }
	if parsedDate, err := time.Parse("2006-01-02", input.Date); err != nil || parsedDate.Format("2006-01-02") != input.Date {
		return domain.Expense{}, "", ErrInvalidExpense
	}
	monthKey, data := monthData(input.Month)
	expense := domain.Expense{ID: newID(), Name: input.Name, Amount: input.Amount, Date: input.Date, Category: input.Category}
	if input.BudgetID != "" {
		found := false
		for i := range data.Budgets {
			if data.Budgets[i].ID != input.BudgetID { continue }
			expense.Category = categoryOr(data.Budgets[i].Category, data.Budgets[i].Name)
			data.Budgets[i].Expenses = append(data.Budgets[i].Expenses, expense)
			found = true
			break
		}
		if !found { return domain.Expense{}, "", ErrBudgetNotFound }
	} else {
		if expense.Category == "" { expense.Category = "Allgemein" }
		data.StandardExpenses = append(data.StandardExpenses, expense)
	}
	state.Months[monthKey] = data
	return expense, monthKey, nil
}

func categoryOr(category, fallback string) string {
	if strings.TrimSpace(category) != "" { return category }
	return fallback
}
