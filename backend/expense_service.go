package backend

import (
	"strings"

	expenseservice "finly/backend/services"
)

var errBudgetNotFound = expenseservice.ErrBudgetNotFound
var errInvalidExpense = expenseservice.ErrInvalidExpense

type CreateExpenseInput = expenseservice.CreateExpenseInput

// CreateExpense is shared by browser forms and the authenticated API. The
// business rules live in backend/services; this method handles locking and
// persistence for the active tenant.
func (a *App) CreateExpense(input CreateExpenseInput) (Expense, string, error) {
	a.mu.Lock()
	input.Month = normalizeActiveMonth(a.data.ActiveMonth)
	if input.ApplyAPIMapping {
		for _, mapping := range a.data.APIMappings {
			matched := false
			for _, name := range mapping.Names {
				if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(input.Name)) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			input.Category = mapping.Category
			input.BudgetID = ""
			_, month := a.monthDataLocked(input.Month)
			for _, budget := range month.Budgets {
				if strings.EqualFold(strings.TrimSpace(categoryOr(budget.Category, budget.Name)), strings.TrimSpace(mapping.Category)) {
					input.BudgetID = budget.ID
					break
				}
			}
			break
		}
	}
	monthKey := parseMonth(input.Month).Format("2006-01")
	previous, existed := a.data.Months[monthKey]
	expense, monthKey, err := expenseservice.CreateExpense(&a.data, input, a.monthDataLocked, newID)
	if err != nil { a.mu.Unlock(); return Expense{}, "", err }
	if err := a.saveLocked(); err != nil {
		if existed { a.data.Months[monthKey] = previous } else { delete(a.data.Months, monthKey) }
		a.mu.Unlock()
		return Expense{}, "", err
	}
	a.mu.Unlock()
	return expense, monthKey, nil
}
