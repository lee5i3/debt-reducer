// Package pages renders each page of the app (Plan, Simulate, Debts,
// Settings, errors) from its view model, using shared components.
package pages

import (
	"fmt"
	"github.com/lee5i3/debt-reducer/web/views/components"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/lee5i3/debt-reducer/internal/debt"
)

// DebtsData is the view model for the debts page and its dialogs.
type DebtsData struct {
	Shell        components.Shell
	Debts        []debt.Debt         // after the search filter; Balance is what's left
	Started      map[int64]debt.Debt // each debt as entered: starting amount and date
	Inactive     int                 // debts left out of the plan (not in the totals)
	TotalCount   int                 // all debts
	Query        string
	TotalBalance debt.Money
	MinimumTotal debt.Money
	Notice       string

	// Dialog is the popup shown over the list: "", "form", "detail" or "delete".
	Dialog string
	Form   DebtForm
	Detail DebtDetail
	Target debt.Debt // the debt being deleted
}

// DebtForm holds raw form input so invalid submissions can be re-rendered.
type DebtForm struct {
	ID             int64
	Name           string
	Type           string
	Balance        string
	APR            string
	Purchases      []PurchaseRow     // promotional purchase account: each purchase
	PurchaseTotal  string            // sum of what remains on the purchases
	TermMonths     string            // loan months remaining (optional)
	MinimumPayment string            // monthly payment (entered or calculated)
	DueDay         string            // "" = not set
	StartDate      string            // YYYY-MM-DD the balance is as of
	Active         bool              // included in the plan and Simulate
	Errors         map[string]string // per field, keyed by input name
	Error          string            // anything not tied to one field
}

// PurchaseRow is one purchase on a promotional account, as typed.
type PurchaseRow struct {
	Remaining string
	Ends      string // YYYY-MM-DD the promotion expires
	Error     string
}

// Editing reports whether the form edits an existing debt.
func (f DebtForm) Editing() bool { return f.ID != 0 }

// Err returns the error message for one field, or "".
func (f DebtForm) Err(field string) string { return f.Errors[field] }

// FormFromDebt pre-fills the form for editing.
func FormFromDebt(d debt.Debt, today time.Time) DebtForm {
	f := DebtForm{
		ID:      d.ID,
		Active:  !d.Inactive,
		Name:    d.Name,
		Type:    d.Type,
		Balance: strings.TrimPrefix(d.Balance.String(), "$"),
		APR:     strconv.FormatFloat(d.APR, 'f', 2, 64),
	}
	for _, p := range d.Promos {
		f.Purchases = append(f.Purchases, PurchaseRow{
			Remaining: strings.TrimPrefix(p.Remaining.String(), "$"),
			Ends:      p.Ends.Format(time.DateOnly),
		})
	}
	if len(f.Purchases) == 0 && d.Type == debt.TypePromo && d.TermEnds != nil {
		// A promotion saved before purchases were tracked separately.
		f.Purchases = []PurchaseRow{{Remaining: f.Balance, Ends: d.TermEnds.Format(time.DateOnly)}}
	}
	if len(f.Purchases) == 0 {
		f.Purchases = []PurchaseRow{{}}
	}
	if d.Type == debt.TypePromo {
		f.PurchaseTotal = d.Balance.String()
	}
	if d.Type == debt.TypeLoan {
		if m := debt.MonthsUntil(d.TermEnds, today); m > 0 {
			f.TermMonths = strconv.Itoa(m)
		}
	}
	f.MinimumPayment = strings.TrimPrefix(d.MinimumPayment.String(), "$")
	if !d.StartDate.IsZero() {
		f.StartDate = d.StartDate.Format(time.DateOnly)
	}
	if d.DueDay > 0 {
		f.DueDay = strconv.Itoa(d.DueDay)
	}
	return f
}

// namePlaceholder suggests a name for the chosen type.
func namePlaceholder(t string) string {
	switch t {
	case debt.TypeLoan:
		return "e.g. Car loan"
	case debt.TypePromo:
		return "e.g. Furniture financing"
	default:
		return "e.g. Visa card"
	}
}

// aprText shows "0% until Apr 2029" for a running promotion, else the APR.
func aprText(d debt.Debt) string {
	now := time.Now()
	if !d.InterestFree(now) {
		return percent(d.APR)
	}
	if next := d.NextPromoEnd(now); next != nil {
		return "0% until " + next.Format("Jan 2006")
	}
	return fmt.Sprintf("0%% for %d mo", debt.MonthsUntil(d.TermEnds, now))
}

var dueDays = func() []string {
	days := make([]string, 31)
	for i := range days {
		days[i] = strconv.Itoa(i + 1)
	}
	return days
}()

// DebtDetail is the data in the "view details" dialog.
type DebtDetail struct {
	Debt              debt.Debt // with what's left
	Started           debt.Debt // as entered: starting amount and date
	InterestThisMonth debt.Money
	SharePercent      int
	PayoffMonths      int
	PayoffOK          bool // false when the minimum doesn't cover interest

	// From the saved plan: the month this debt is paid off and how many
	// plan payments that takes. PlanSet is false before the plan is set up.
	PlanSet      bool
	PlanPayoff   time.Time // zero if the plan doesn't pay it off
	PlanPayments int
}

// MinimumInterest is the part of this month's minimum that goes to interest.
func (x DebtDetail) MinimumInterest() debt.Money {
	return min(x.InterestThisMonth, x.Debt.MinimumPayment)
}

// OffBalance is the part of this month's minimum that lowers the balance.
func (x DebtDetail) OffBalance() debt.Money {
	return x.Debt.MinimumPayment - x.MinimumInterest()
}

// InterestPercent is the interest share of the minimum, 0-100.
func (x DebtDetail) InterestPercent() int {
	if x.Debt.MinimumPayment <= 0 {
		return 0
	}
	return int(float64(x.MinimumInterest()) / float64(x.Debt.MinimumPayment) * 100)
}

func ariaInvalid(msg string) string {
	if msg != "" {
		return "true"
	}
	return "false"
}

// planPayoffText is "Jul 2028 · 20 payments" for the details popup.
func planPayoffText(x DebtDetail) string {
	switch {
	case x.Debt.Balance == 0:
		return "Paid off"
	case x.Debt.Inactive:
		return "Inactive, not in your plan"
	case x.PlanPayoff.IsZero():
		return "Not within your plan"
	case x.PlanPayments == 1:
		return x.PlanPayoff.Format("Jan 2006") + " · next payment"
	default:
		return fmt.Sprintf("%s · %d payments", x.PlanPayoff.Format("Jan 2006"), x.PlanPayments)
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

// tomorrow is the earliest date a promotion can end, for the date picker.
func tomorrow() string { return time.Now().AddDate(0, 0, 1).Format(time.DateOnly) }

// moneyText formats an amount for an input box, e.g. "1,200.00".
func moneyText(m debt.Money) string { return strings.TrimPrefix(m.String(), "$") }

// promoTip says when a promotional account's purchases expire, one per
// line, e.g. "Apr 13, 2029: $1,409.38 left".
func promoTip(d debt.Debt) []string {
	var lines []string
	for _, p := range d.Promos {
		if p.Remaining > 0 {
			lines = append(lines, p.Ends.Format("Jan 2, 2006")+": "+p.Remaining.String()+" left")
		}
	}
	if len(lines) == 0 && d.TermEnds != nil {
		lines = append(lines, d.TermEnds.Format("Jan 2, 2006"))
	}
	return lines
}

// startedText is "Started at $4,500.00 · Nov 21, 2026".
func startedText(d debt.Debt) string {
	t := "Started at " + d.Balance.String()
	if !d.StartDate.IsZero() {
		t += " · " + d.StartDate.Format("Jan 2, 2006")
	}
	return t
}

// todayISO is today's date for the "as of" date picker.
func todayISO() string { return time.Now().Format(time.DateOnly) }

// debtsSummary is the line under the Debts heading. Inactive debts are
// listed but not counted in the totals.
func debtsSummary(d DebtsData) string {
	t := fmt.Sprintf("%d debts · %s owed · %s/mo in minimums", d.TotalCount, d.TotalBalance, d.MinimumTotal)
	if d.Inactive > 0 {
		t += fmt.Sprintf(" · %d inactive, not counted", d.Inactive)
	}
	return t
}
