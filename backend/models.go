package backend

import (
	"database/sql"
	"sync"
	"time"

	"finly/backend/domain"
)

type FixedCost = domain.FixedCost
type Expense = domain.Expense
type Budget = domain.Budget
type APIMapping = domain.APIMapping
type MonthData = domain.MonthData
type Store = domain.Store
type AuthConfig = domain.AuthConfig
type CategoryMetric = domain.CategoryMetric

type Page struct {
	Month                                                   time.Time
	MonthKey, MonthLabel, MonthShort, NextMonthLabel, Tab   string
	AnalyticsPeriod, AnalyticsLabel, AnalyticsMonth, AnalyticsYear string
	Income, FixedTotal, FreeTotal, BudgetTotal              float64
	SpentTotal, StandardSpent, Remaining, AnalyticsTotal    float64
	FixedCosts                                              []FixedCost
	Budgets                                                 []Budget
	StandardExpenses                                        []Expense
	APIMappings                                             []APIMapping
	Analytics                                               []CategoryMetric
	Flash                                                   string
	APIKeyConfigured                                        bool
	APIKeyCreatedAt                                         string
	AccountUsername                                         string
	AccountError                                            string
	AccountNotice                                           string
}

type App struct {
	mu       sync.Mutex
	tenantRequestMu sync.Mutex
	authMu   sync.RWMutex
	db       *sql.DB
	data     Store
	currentTenant string
	legacyData Store
	legacyAvailable bool
	authUsername     string
	authPasswordHash string
	sessionSecret    []byte
	jwtSecret        []byte
	apiKeyHash       string
	apiKeyCreatedAt  string
	secureCookie    bool
}

func NewApp(db *sql.DB) *App {
	return &App{
		db: db,
		data: Store{Months: make(map[string]MonthData)},
	}
}
