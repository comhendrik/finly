package backend

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func (a *App) home(c *gin.Context) {
	a.mu.Lock()
	monthKey := normalizeActiveMonth(a.data.ActiveMonth)
	a.data.ActiveMonth = monthKey
	month := parseMonth(monthKey)
	monthData, exists := a.data.Months[monthKey]
	if !exists {
		monthData = MonthData{Budgets: []Budget{}, StandardExpenses: []Expense{}}
	}

	income := a.data.Income
	fixedCosts := append([]FixedCost{}, a.data.FixedCosts...)
	if monthData.Initialized {
		income = monthData.Income
		fixedCosts = append([]FixedCost{}, monthData.FixedCosts...)
	} else {
		income, fixedCosts = a.defaultsForMonthLocked(monthKey)
	}

	monthData.Budgets = cloneBudgets(monthData.Budgets)
	monthData.StandardExpenses = append([]Expense{}, monthData.StandardExpenses...)
	apiMappings := append([]APIMapping{}, a.data.APIMappings...)
	a.mu.Unlock()

	page := buildPage(month, monthKey, monthData, income, fixedCosts, c.Query("tab"), c.Query("saved"))
	page.APIMappings = apiMappings
	a.authMu.RLock()
	page.APIKeyConfigured = a.apiKeyHash != ""
	page.APIKeyCreatedAt = a.apiKeyCreatedAt
	page.AccountUsername = a.authUsername
	a.authMu.RUnlock()
	page.AccountError = c.Query("account_error")
	page.AccountNotice = c.Query("account_notice")
	page.AnalyticsPeriod = analyticsPeriod(c.Query("analytics_period"))
	page.AnalyticsMonth = parseSelectedMonth(c.Query("analytics_month"), month).Format("2006-01")
	page.AnalyticsYear = selectedYear(c.Query("analytics_year"), month.Year())
	page.AnalyticsLabel = analyticsLabel(page.AnalyticsPeriod, page.AnalyticsMonth, page.AnalyticsYear)

	if page.Tab == "analytics" {
		selectedMonth := parseMonth(page.AnalyticsMonth)
		selectedYear, _ := strconv.Atoi(page.AnalyticsYear)
		page.Analytics, page.AnalyticsTotal = a.categoryMetrics(page.AnalyticsPeriod, selectedMonth, selectedYear)
	}

	c.HTML(http.StatusOK, "index.html", page)
}

func buildPage(month time.Time, monthKey string, monthData MonthData, income float64, fixedCosts []FixedCost, tab, flash string) Page {
	tab = normalizeTab(tab)
	var fixedTotal, budgetTotal, budgetExpenses, standardSpent float64

	for _, fixed := range fixedCosts {
		fixedTotal += fixed.Amount
	}
	for _, budget := range monthData.Budgets {
		budgetTotal += budget.Amount
		for _, expense := range budget.Expenses {
			budgetExpenses += expense.Amount
		}
	}
	for _, expense := range monthData.StandardExpenses {
		standardSpent += expense.Amount
	}

	freeAfterFixed := income - fixedTotal
	if freeAfterFixed < 0 {
		freeAfterFixed = 0
	}

	return Page{
		Month:             month,
		MonthKey:          monthKey,
		MonthLabel:        monthLabel(month),
		MonthShort:        monthShort(month),
		NextMonthLabel:    monthLabel(month.AddDate(0, 1, 0)),
		Tab:               tab,
		Income:            income,
		FixedTotal:        fixedTotal,
		FreeTotal:         freeAfterFixed,
		BudgetTotal:       budgetTotal,
		SpentTotal:        budgetExpenses + standardSpent,
		StandardSpent:     standardSpent,
		Remaining:         freeAfterFixed - budgetTotal - standardSpent,
		FixedCosts:        fixedCosts,
		Budgets:           monthData.Budgets,
		StandardExpenses:  monthData.StandardExpenses,
		Flash:             flash,
	}
}

func cloneBudgets(budgets []Budget) []Budget {
	cloned := make([]Budget, len(budgets))
	copy(cloned, budgets)
	for i := range cloned {
		cloned[i].Expenses = append([]Expense{}, budgets[i].Expenses...)
	}
	return cloned
}

func normalizeTab(tab string) string {
	switch tab {
	case "settings", "standard", "budgets", "analytics", "account":
		return tab
	default:
		return "overview"
	}
}

func (a *App) changeMonth(c *gin.Context) {
	tab := normalizeTab(c.PostForm("tab"))
	c.Redirect(http.StatusSeeOther, "/?tab="+tab)
}

func (a *App) redirectAfterSave(c *gin.Context) {
	a.mu.Lock()
	month := parseMonth(normalizeActiveMonth(a.data.ActiveMonth))
	a.mu.Unlock()
	tab := normalizeTab(c.PostForm("tab"))
	url := "/?month=" + month.Format("2006-01") + "&tab=" + tab + "&saved=Änderungen+gespeichert"
	c.Redirect(http.StatusSeeOther, url)
}

func analyticsPeriod(raw string) string {
	switch raw {
	case "year", "all":
		return raw
	default:
		return "month"
	}
}

func parseSelectedMonth(raw string, fallback time.Time) time.Time {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	return parseMonth(raw)
}

func selectedYear(raw string, fallback int) string {
	year, err := strconv.Atoi(raw)
	if err != nil || year < 1 {
		year = fallback
	}
	return strconv.Itoa(year)
}

func analyticsLabel(period, month, year string) string {
	switch period {
	case "year":
		return year
	case "all":
		return "Gesamter Zeitraum"
	default:
		return monthLabel(parseMonth(month))
	}
}

func (a *App) categoryMetrics(period string, selectedMonth time.Time, year int) ([]CategoryMetric, float64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	totals := make(map[string]float64)
	addAmount := func(category string, amount float64, fallback string) {
		if amount <= 0 {
			return
		}
		category = categoryOr(category, fallback)
		totals[category] += amount
	}

	for key, month := range a.data.Months {
		date, err := time.Parse("2006-01", key)
		if err != nil || !inAnalyticsRange(period, key, date, selectedMonth, year) {
			continue
		}

		fixedCosts := month.FixedCosts
		if !month.Initialized {
			_, fixedCosts = a.defaultsForMonthLocked(key)
		}
		for _, fixed := range fixedCosts {
			addAmount("Fixkosten", fixed.Amount, "Fixkosten")
		}

		for _, budget := range month.Budgets {
			fallback := categoryOr(budget.Category, budget.Name)
			for _, expense := range budget.Expenses {
				addAmount(expense.Category, expense.Amount, fallback)
			}
		}
		for _, expense := range month.StandardExpenses {
			addAmount(expense.Category, expense.Amount, "Allgemein")
		}
	}

	// Include legacy fixed costs when the selected month has no saved snapshot.
	if period == "month" {
		key := selectedMonth.Format("2006-01")
		if _, exists := a.data.Months[key]; !exists {
			_, fixedCosts := a.defaultsForMonthLocked(key)
			for _, fixed := range fixedCosts {
				addAmount("Fixkosten", fixed.Amount, "Fixkosten")
			}
		}
	}

	metrics := make([]CategoryMetric, 0, len(totals))
	var total float64
	for name, amount := range totals {
		total += amount
		metrics = append(metrics, CategoryMetric{Name: name, Amount: amount})
	}
	for i := range metrics {
		if total > 0 {
			metrics[i].Percent = metrics[i].Amount / total * 100
		}
	}
	sort.Slice(metrics, func(i, j int) bool {
		return metrics[i].Amount > metrics[j].Amount
	})

	return metrics, total
}

func inAnalyticsRange(period, monthKey string, date, selectedMonth time.Time, year int) bool {
	switch period {
	case "month":
		return monthKey == selectedMonth.Format("2006-01")
	case "year":
		return date.Year() == year
	default:
		return true
	}
}

func categoryOr(category, fallback string) string {
	if strings.TrimSpace(category) != "" {
		return strings.TrimSpace(category)
	}
	return fallback
}
