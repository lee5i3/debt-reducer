// Package views contains the templ components that render the web UI.
package views

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/lee5i3/debt-reducer/internal/debt"
)

// IndexData is the view model for the dashboard page.
type IndexData struct {
	Debts         []debt.Debt
	TotalBalance  debt.Money
	MinimumTotal  debt.Money
	Strategy      debt.Strategy
	MonthlyBudget debt.Money
	StartDate     time.Time
	Plan          *debt.Plan
	Comparison    []*debt.Plan
	Baseline      *debt.Plan
	Error         string
	Notice        string
}

// LumpSumData is the view model for the one-time payment page.
type LumpSumData struct {
	Debts         []debt.Debt
	TotalBalance  debt.Money
	Amount        string // raw input, re-shown in the form
	Strategy      debt.Strategy
	MonthlyBudget debt.Money
	StartDate     time.Time
	Analysis      *debt.LumpSumAnalysis
	Error         string
}

func sooner(months int) string {
	switch {
	case months <= 0:
		return "same payoff date"
	case months == 1:
		return "1 month sooner"
	default:
		return fmt.Sprintf("%d months sooner", months)
	}
}

func allocationList(allocs []debt.Allocation) string {
	parts := make([]string, len(allocs))
	for i, a := range allocs {
		parts[i] = a.Debt + " " + a.Amount.String()
	}
	return strings.Join(parts, " · ")
}

func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
}

// DebtsData is the view model for the debts management page.
type DebtsData struct {
	Debts        []debt.Debt
	TotalBalance debt.Money
	MinimumTotal debt.Money
	Form         DebtForm
	Notice       string
}

// DebtForm holds raw form input so invalid submissions can be re-rendered.
type DebtForm struct {
	ID             int64
	Name           string
	Balance        string
	APR            string
	MinimumPayment string
	DueDay         string
	Error          string
}

// Editing reports whether the form edits an existing debt.
func (f DebtForm) Editing() bool { return f.ID != 0 }

// FormFromDebt pre-fills the form for editing.
func FormFromDebt(d debt.Debt) DebtForm {
	return DebtForm{
		ID:             d.ID,
		Name:           d.Name,
		Balance:        dollars(d.Balance),
		APR:            strconv.FormatFloat(d.APR, 'f', -1, 64),
		MinimumPayment: dollars(d.MinimumPayment),
		DueDay:         strconv.Itoa(d.DueDay),
	}
}

func debtURL(id int64, suffix string) templ.SafeURL {
	return templ.SafeURL(fmt.Sprintf("/debts/%d%s", id, suffix))
}

func ordinal(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(n) + suffix
}

func date(t time.Time) string      { return t.Format("Jan 2, 2006") }
func monthYear(t time.Time) string { return t.Format("January 2006") }
func isoDate(t time.Time) string   { return t.Format(time.DateOnly) }
func dollars(m debt.Money) string  { return strconv.FormatFloat(m.Dollars(), 'f', 2, 64) }
func percent(apr float64) string   { return fmt.Sprintf("%.2f%%", apr) }
func itoa(n int) string            { return strconv.Itoa(n) }

// period labels a schedule row by its payment date range, e.g.
// "Nov 28 – Dec 15, 2026" or "Dec 28, 2026 – Jan 15, 2027".
func period(m debt.Month) string {
	switch {
	case m.Date.Equal(m.EndDate):
		return date(m.Date)
	case m.Date.Year() == m.EndDate.Year():
		return m.Date.Format("Jan 2") + " – " + date(m.EndDate)
	default:
		return date(m.Date) + " – " + date(m.EndDate)
	}
}

func duration(months int) string {
	y, m := months/12, months%12
	switch {
	case y == 0:
		return fmt.Sprintf("%d mo", m)
	case m == 0:
		return fmt.Sprintf("%d yr", y)
	default:
		return fmt.Sprintf("%d yr %d mo", y, m)
	}
}

// strategyURL links to the dashboard with the same inputs but another strategy.
func strategyURL(d IndexData, s debt.Strategy) templ.SafeURL {
	q := url.Values{}
	q.Set("strategy", string(s))
	q.Set("budget", dollars(d.MonthlyBudget))
	q.Set("start", isoDate(d.StartDate))
	return templ.SafeURL("/?" + q.Encode())
}
