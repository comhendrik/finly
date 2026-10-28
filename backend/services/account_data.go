package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"finly/backend/domain"
)

const dataExportFormat = "finly-user-data"
const maxDataExportMonths = 240

type DataExport struct {
	Format    string                     `json:"format"`
	Version   int                        `json:"version"`
	FromMonth string                     `json:"from_month"`
	ToMonth   string                     `json:"to_month"`
	ExportedAt time.Time                  `json:"exported_at"`
	Months    map[string]domain.MonthData `json:"months"`
}

func ExportData(state domain.Store, fromRaw, toRaw string, defaults func(string) (float64, []domain.FixedCost)) (DataExport, error) {
	from, to, err := ParseMonthRange(fromRaw, toRaw)
	if err != nil { return DataExport{}, err }
	result := DataExport{
		Format: dataExportFormat,
		Version: 1,
		FromMonth: from.Format("2006-01"),
		ToMonth: to.Format("2006-01"),
		ExportedAt: time.Now().UTC(),
		Months: make(map[string]domain.MonthData),
	}
	for month := from; !month.After(to); month = month.AddDate(0, 1, 0) {
		key := month.Format("2006-01")
		data := state.Months[key]
		if !data.Initialized {
			data.Income, data.FixedCosts = defaults(key)
			data.Initialized = true
		}
		if data.Budgets == nil { data.Budgets = []domain.Budget{} }
		if data.StandardExpenses == nil { data.StandardExpenses = []domain.Expense{} }
		if data.FixedCosts == nil { data.FixedCosts = []domain.FixedCost{} }
		result.Months[key] = data
	}
	return result, nil
}

func ParseMonthRange(fromRaw, toRaw string) (time.Time, time.Time, error) {
	from, fromErr := time.Parse("2006-01", fromRaw)
	to, toErr := time.Parse("2006-01", toRaw)
	if fromErr != nil || toErr != nil || from.Format("2006-01") != fromRaw || to.Format("2006-01") != toRaw || to.Before(from) {
		return time.Time{}, time.Time{}, errors.New("ungültige Monatsspanne")
	}
	months := (to.Year()-from.Year())*12 + int(to.Month()-from.Month()) + 1
	if months > maxDataExportMonths { return time.Time{}, time.Time{}, fmt.Errorf("die Monatsspanne darf höchstens %d Monate umfassen", maxDataExportMonths) }
	return from, to, nil
}

func DecodeDataExport(reader io.Reader) (DataExport, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var data DataExport
	if err := decoder.Decode(&data); err != nil { return DataExport{}, fmt.Errorf("ungültige JSON-Datei: %w", err) }
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) { return DataExport{}, errors.New("die Datei enthält zusätzliche JSON-Daten") }
	if data.Format != dataExportFormat || data.Version != 1 { return DataExport{}, errors.New("unbekanntes Finly-Exportformat") }
	from, to, err := ParseMonthRange(data.FromMonth, data.ToMonth)
	if err != nil { return DataExport{}, err }
	expected := (to.Year()-from.Year())*12 + int(to.Month()-from.Month()) + 1
	if len(data.Months) != expected { return DataExport{}, errors.New("die Datei muss jeden Monat der angegebenen Spanne enthalten") }
	for month := from; !month.After(to); month = month.AddDate(0, 1, 0) {
		key := month.Format("2006-01")
		monthData, exists := data.Months[key]
		if !exists { return DataExport{}, fmt.Errorf("Monat %s fehlt in der Datei", key) }
		if err := validateMonth(monthData); err != nil { return DataExport{}, fmt.Errorf("Monat %s: %w", key, err) }
		monthData.Initialized = true
		if monthData.Budgets == nil { monthData.Budgets = []domain.Budget{} }
		if monthData.StandardExpenses == nil { monthData.StandardExpenses = []domain.Expense{} }
		if monthData.FixedCosts == nil { monthData.FixedCosts = []domain.FixedCost{} }
		for i := range monthData.Budgets {
			if monthData.Budgets[i].Expenses == nil { monthData.Budgets[i].Expenses = []domain.Expense{} }
		}
		data.Months[key] = monthData
	}
	return data, nil
}

func ApplyDataExport(state *domain.Store, data DataExport) {
	for key, month := range data.Months { state.Months[key] = month }
}

func validateMonth(data domain.MonthData) error {
	if data.Income < 0 { return errors.New("Einkommen darf nicht negativ sein") }
	for _, fixed := range data.FixedCosts {
		if strings.TrimSpace(fixed.ID) == "" || strings.TrimSpace(fixed.Name) == "" || fixed.Amount <= 0 { return errors.New("ungültige Fixkosten") }
	}
	for _, budget := range data.Budgets {
		if strings.TrimSpace(budget.ID) == "" || strings.TrimSpace(budget.Name) == "" || budget.Amount <= 0 { return errors.New("ungültiges Budget") }
		for _, expense := range budget.Expenses { if err := validateExpense(expense); err != nil { return err } }
	}
	for _, expense := range data.StandardExpenses { if err := validateExpense(expense); err != nil { return err } }
	return nil
}

func validateExpense(expense domain.Expense) error {
	if strings.TrimSpace(expense.ID) == "" || strings.TrimSpace(expense.Name) == "" || expense.Amount <= 0 { return errors.New("ungültige Ausgabe") }
	date, err := time.Parse("2006-01-02", expense.Date)
	if err != nil || date.Format("2006-01-02") != expense.Date { return errors.New("ungültiges Ausgabedatum") }
	return nil
}

