package domain

type FixedCost struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
}

type Expense struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Amount   float64 `json:"amount"`
	Date     string  `json:"date"`
	Category string  `json:"category,omitempty"`
}

type Budget struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Amount   float64   `json:"amount"`
	Category string    `json:"category,omitempty"`
	Expenses []Expense `json:"expenses"`
}

type APIMapping struct {
	ID         string   `json:"id"`
	Category   string   `json:"category"`
	Names      []string `json:"names"`
	LegacyName string   `json:"name,omitempty"`
}

type MonthData struct {
	Budgets          []Budget    `json:"budgets"`
	StandardExpenses []Expense   `json:"standard_expenses"`
	Income           float64     `json:"income"`
	FixedCosts       []FixedCost `json:"fixed_costs"`
	Initialized      bool        `json:"initialized"`
}

type Store struct {
	ActiveMonth string               `json:"active_month,omitempty"`
	APIMappings []APIMapping         `json:"api_mappings,omitempty"`
	// Income and FixedCosts remain to support imports from older JSON files.
	Income     float64              `json:"income"`
	FixedCosts []FixedCost          `json:"fixed_costs"`
	Months     map[string]MonthData `json:"months"`
	Auth       *AuthConfig          `json:"auth,omitempty"`
}

type AuthConfig struct {
	Username        string `json:"username"`
	PasswordHash    string `json:"password_hash"`
	SessionKey      string `json:"session_key"`
	JWTSecret       string `json:"jwt_secret"`
	APIKeyHash      string `json:"api_key_hash,omitempty"`
	APIKeyCreatedAt string `json:"api_key_created_at,omitempty"`
}

type CategoryMetric struct {
	Name    string
	Amount  float64
	Percent float64
}
