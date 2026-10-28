package backend

import (
	"strings"

	"github.com/gin-gonic/gin"
)

func (a *App) saveSettings(c *gin.Context) {
	if err := c.Request.ParseForm(); err != nil {
		c.String(400, "invalid form data")
		return
	}
	values := c.Request.PostForm

	valueAt := func(key string, index int) string {
		if index >= len(values[key]) {
			return ""
		}
		return values[key][index]
	}

	deletedFixed := stringSet(values["delete_fixed_id"])
	deletedBudgets := stringSet(values["delete_budget_id"])
	fixedIndexes := indexByID(values["fixed_id"])
	budgetIndexes := indexByID(values["budget_id"])

	a.mu.Lock()
	monthKey, month := a.monthDataLocked(c.PostForm("month"))
	month.Income = parseAmount(c.PostForm("income"))
	month.FixedCosts = updateFixedCosts(month.FixedCosts, values, fixedIndexes, deletedFixed, valueAt)
	month.Budgets = updateBudgets(month.Budgets, values, budgetIndexes, deletedBudgets, valueAt)

	a.saveMonthAndRedirect(c, monthKey, month)
}

func updateFixedCosts(
	current []FixedCost,
	values map[string][]string,
	indexes map[string]int,
	deleted map[string]bool,
	valueAt func(string, int) string,
) []FixedCost {
	updated := make([]FixedCost, 0, len(current)+len(values["new_fixed_name"]))

	for _, fixed := range current {
		if deleted[fixed.ID] {
			continue
		}
		if index, exists := indexes[fixed.ID]; exists {
			name := strings.TrimSpace(valueAt("fixed_name", index))
			amount := parseAmount(valueAt("fixed_amount", index))
			if name != "" && amount > 0 {
				fixed.Name = name
				fixed.Amount = amount
				fixed.Category = strings.TrimSpace(valueAt("fixed_category", index))
			}
		}
		updated = append(updated, fixed)
	}

	for i, name := range values["new_fixed_name"] {
		name = strings.TrimSpace(name)
		amount := parseAmount(valueAt("new_fixed_amount", i))
		if name == "" || amount <= 0 {
			continue
		}

		updated = append(updated, FixedCost{
			ID:       newID(),
			Name:     name,
			Amount:   amount,
			Category: strings.TrimSpace(valueAt("new_fixed_category", i)),
		})
	}

	return updated
}

func updateBudgets(
	current []Budget,
	values map[string][]string,
	indexes map[string]int,
	deleted map[string]bool,
	valueAt func(string, int) string,
) []Budget {
	updated := make([]Budget, 0, len(current)+len(values["new_budget_name"]))

	for _, budget := range current {
		if deleted[budget.ID] {
			continue
		}
		if index, exists := indexes[budget.ID]; exists {
			name := strings.TrimSpace(valueAt("budget_name", index))
			amount := parseAmount(valueAt("budget_amount", index))
			if name != "" && amount > 0 {
				budget.Name = name
				budget.Amount = amount
				budget.Category = strings.TrimSpace(valueAt("budget_category", index))
			}
		}
		updated = append(updated, budget)
	}

	for i, name := range values["new_budget_name"] {
		name = strings.TrimSpace(name)
		amount := parseAmount(valueAt("new_budget_amount", i))
		if name == "" || amount <= 0 {
			continue
		}

		updated = append(updated, Budget{
			ID:       newID(),
			Name:     name,
			Amount:   amount,
			Category: strings.TrimSpace(valueAt("new_budget_category", i)),
			Expenses: []Expense{},
		})
	}

	return updated
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func indexByID(ids []string) map[string]int {
	indexes := make(map[string]int, len(ids))
	for index, id := range ids {
		indexes[id] = index
	}
	return indexes
}
