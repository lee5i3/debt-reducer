package components

import (
	"math"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/lee5i3/debt-reducer/internal/debt"
)

// MoneyField configures MoneyInput.
type MoneyField struct {
	ID      string
	Name    string
	Value   string // as typed or saved; formatted on render when it's a number
	Invalid bool
	Short   bool             // narrow box for small amounts
	Attrs   templ.Attributes // extra attributes (autofocus, data-*, aria-*)
}

// FormatMoney turns "5000" or "$1234.5" into "5,000.00" / "1,234.50".
// Anything that isn't a number is returned unchanged so the user can fix it.
func FormatMoney(raw string) string {
	raw = strings.TrimSpace(raw)
	clean := strings.NewReplacer("$", "", ",", "", " ", "").Replace(raw)
	if clean == "" {
		return ""
	}
	f, err := strconv.ParseFloat(clean, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return raw
	}
	return strings.Replace(debt.FromDollars(f).String(), "$", "", 1)
}

func boolText(b bool) string { return strconv.FormatBool(b) }
