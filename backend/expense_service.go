package backend

import expenseservice "finly/backend/services"

var errBudgetNotFound = expenseservice.ErrBudgetNotFound
var errInvalidExpense = expenseservice.ErrInvalidExpense

type CreateExpenseInput = expenseservice.CreateExpenseInput

// CreateExpense is shared by browser forms and the authenticated API. The
// business rules live in backend/services; this method handles locking and
// persistence for the active tenant.
func (a *App) CreateExpense(input CreateExpenseInput) (Expense, string, error) {
	a.mu.Lock()
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
