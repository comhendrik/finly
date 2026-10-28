package backend

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func parseMonth(raw string) time.Time {
	month, err := time.Parse("2006-01", raw)
	if err != nil {
		return time.Now()
	}
	return month
}

func monthLabel(month time.Time) string {
	names := []string{
		"Januar", "Februar", "März", "April", "Mai", "Juni",
		"Juli", "August", "September", "Oktober", "November", "Dezember",
	}
	return fmt.Sprintf("%s %d", names[month.Month()-1], month.Year())
}

func monthShort(month time.Time) string {
	names := []string{
		"Jan", "Feb", "Mär", "Apr", "Mai", "Jun",
		"Jul", "Aug", "Sep", "Okt", "Nov", "Dez",
	}
	return fmt.Sprintf("%s %d", names[month.Month()-1], month.Year())
}

func parseAmount(raw string) float64 {
	value, _ := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
	return value
}

func newID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}
