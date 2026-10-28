package backend

import (
	"strings"
	"time"

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
	deletedMappings := stringSet(values["delete_mapping_id"])
	fixedIndexes := indexByID(values["fixed_id"])
	budgetIndexes := indexByID(values["budget_id"])
	mappingIndexes := indexByID(values["mapping_id"])

	a.mu.Lock()
	monthKey := normalizeActiveMonth(a.data.ActiveMonth)
	previousMonth, monthExisted := a.data.Months[monthKey]
	previousMappings := a.data.APIMappings
	monthKey, month := a.monthDataLocked(a.data.ActiveMonth)
	month.Income = parseAmount(c.PostForm("income"))
	month.FixedCosts = updateFixedCosts(month.FixedCosts, values, fixedIndexes, deletedFixed, valueAt)
	month.Budgets = updateBudgets(month.Budgets, values, budgetIndexes, deletedBudgets, valueAt)
	a.data.APIMappings = updateAPIMappings(a.data.APIMappings, values, mappingIndexes, deletedMappings, valueAt)
	a.data.Months[monthKey] = month
	if err := a.saveLocked(); err != nil {
		if monthExisted {
			a.data.Months[monthKey] = previousMonth
		} else {
			delete(a.data.Months, monthKey)
		}
		a.data.APIMappings = previousMappings
		a.mu.Unlock()
		c.String(500, "could not save settings")
		return
	}
	a.mu.Unlock()
	a.redirectAfterSave(c)
}

func (a *App) setActiveMonth(c *gin.Context) {
	rawMonth := c.PostForm("active_month")
	month, err := time.Parse("2006-01", rawMonth)
	if err != nil || month.Format("2006-01") != rawMonth {
		c.String(400, "invalid month")
		return
	}
	a.mu.Lock()
	previous := normalizeActiveMonth(a.data.ActiveMonth)
	previousActiveMonth := a.data.ActiveMonth
	previousTarget, targetExisted := a.data.Months[rawMonth]
	if rawMonth != previous {
		target := a.data.Months[rawMonth]
		if !target.Initialized {
			_, source := a.monthDataLocked(previous)
			target.Income, target.FixedCosts = a.defaultsForMonthLocked(rawMonth)
			target.FixedCosts = append([]FixedCost{}, source.FixedCosts...)
			target.Initialized = true
			if target.Budgets == nil {
				target.Budgets = []Budget{}
			}
			if target.StandardExpenses == nil {
				target.StandardExpenses = []Expense{}
			}
			a.data.Months[rawMonth] = target
		}
	}
	a.data.ActiveMonth = rawMonth
	if err := a.saveLocked(); err != nil {
		a.data.ActiveMonth = previousActiveMonth
		if targetExisted {
			a.data.Months[rawMonth] = previousTarget
		} else {
			delete(a.data.Months, rawMonth)
		}
		a.mu.Unlock()
		c.String(500, "could not save active month")
		return
	}
	a.mu.Unlock()
	c.Redirect(303, "/?tab=settings&saved=Aktiver+Monat+geändert")
}

func (a *App) deleteMonthData(c *gin.Context) {
	rawMonth := c.PostForm("month")
	month, err := time.Parse("2006-01", rawMonth)
	if err != nil || month.Format("2006-01") != rawMonth {
		c.String(400, "invalid month")
		return
	}
	a.mu.Lock()
	previous, existed := a.data.Months[rawMonth]
	if rawMonth == normalizeActiveMonth(a.data.ActiveMonth) {
		a.data.Months[rawMonth] = MonthData{
			Budgets: []Budget{}, StandardExpenses: []Expense{}, FixedCosts: []FixedCost{}, Initialized: true,
		}
	} else {
		delete(a.data.Months, rawMonth)
	}
	if err := a.saveLocked(); err != nil {
		if existed {
			a.data.Months[rawMonth] = previous
		} else {
			delete(a.data.Months, rawMonth)
		}
		a.mu.Unlock()
		c.String(500, "could not delete month data")
		return
	}
	a.mu.Unlock()
	c.Redirect(303, "/?tab=settings&saved=Monatsdaten+gelöscht")
}

func updateAPIMappings(current []APIMapping, values map[string][]string, indexes map[string]int, deleted map[string]bool, valueAt func(string, int) string) []APIMapping {
	updated := make([]APIMapping, 0, len(current)+len(values["new_mapping_category"]))
	appendMapping := func(mapping APIMapping) {
		mapping.Category = strings.TrimSpace(mapping.Category)
		mapping.Names = normalizeMappingNames(mapping.Names)
		if mapping.Category == "" || len(mapping.Names) == 0 {
			return
		}
		updated = append(updated, mapping)
	}
	for _, mapping := range current {
		if deleted[mapping.ID] {
			continue
		}
		if index, exists := indexes[mapping.ID]; exists {
			mapping.Category = valueAt("mapping_category", index)
			mapping.Names = splitMappingNames(valueAt("mapping_names", index))
		}
		appendMapping(mapping)
	}
	for i, category := range values["new_mapping_category"] {
		appendMapping(APIMapping{ID: newID(), Category: category, Names: splitMappingNames(valueAt("new_mapping_names", i))})
	}
	updated, _ = normalizeAPIMappings(updated, false)
	return updated
}

func splitMappingNames(raw string) []string {
	return strings.FieldsFunc(raw, func(char rune) bool {
		return char == '\n' || char == '\r' || char == ',' || char == ';'
	})
}

func normalizeMappingNames(names []string) []string {
	result := make([]string, 0, len(names))
	seen := make(map[string]bool)
	for _, name := range names {
		name = strings.TrimSpace(name)
		key := strings.ToLower(name)
		if name == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, name)
	}
	return result
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
			ID:     newID(),
			Name:   name,
			Amount: amount,
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
