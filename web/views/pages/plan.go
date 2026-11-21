package pages

import (
	"fmt"
	"github.com/lee5i3/debt-reducer/web/views/components"
	"net/url"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/lee5i3/debt-reducer/internal/debt"
	"github.com/lee5i3/debt-reducer/internal/service"
)

// PlanData is the view model for the tracked plan page and its dialogs.
type PlanData struct {
	ExpandCurrent bool // open the current month by default
	Shell         components.Shell
	Plan          *service.TrackedPlan
	Rows          []service.PlanMonth // months shown
	Hidden        int                 // months not shown until "Show all"
	ShowAll       bool
	Notice        string
	Error         string

	// Dialog is the popup shown over the plan: "", "setup" or "edit".
	Dialog string
	Setup  SetupForm
	Edit   EditForm
}

// SetupForm is the plan setup form (inline before setup, a popup after).
type SetupForm struct {
	Strategy debt.Strategy
	Order    debt.Order
	Pay      string // "min" (just the minimums) or "extra" (minimums plus Extra)
	Extra    string // amount on top of the minimums, as typed
	Note     string // shown above the choice, e.g. an old fixed total below the minimums
	Start    string // YYYY-MM
	Locked   bool   // first month can't change once a month is marked
	Months   []Option
	MinTotal debt.Money
	Error    string
}

// EditForm edits one upcoming month's payment.
type EditForm struct {
	Month   time.Time
	Planned debt.Money
	Plan    debt.Money // the month's amount from plan setup, before edits
	Rows    []EditRow  // what each account gets this month
	Edited  bool
	Extra   debt.Money // one-time payment also in this month
	Error   string
}

// EditRow is one account in the edit-payment dialog.
type EditRow struct {
	DebtID    int64
	Name      string
	Minimum   debt.Money // this month's minimum (capped at what's owed)
	WithExtra bool       // "Min + extra" chosen
	Extra     string     // extra on top of the minimum, as typed
	Error     string
}

// rowTotal is the payment a row adds up to (minimum plus a valid extra).
func (r EditRow) rowTotal() debt.Money {
	if !r.WithExtra {
		return r.Minimum
	}
	x, err := debt.ParseDollars(r.Extra)
	if err != nil || x < 0 {
		return r.Minimum
	}
	return r.Minimum + x
}

// MinTotal is the sum of the accounts' minimums.
func (e EditForm) MinTotal() debt.Money {
	var t debt.Money
	for _, r := range e.Rows {
		t += r.Minimum
	}
	return t
}

// ExtraTotal is what's chosen on top of the minimums.
func (e EditForm) ExtraTotal() debt.Money { return e.Total() - e.MinTotal() }

// PlanStatus compares the total with the plan: a CSS class and a sentence.
func (e EditForm) PlanStatus() (class, text string) {
	total, plan := e.Total(), e.Plan
	switch {
	case total == plan:
		return "is-match", "Matches your " + plan.String() + " plan."
	case total > plan:
		return "is-more", (total - plan).String() + " more than your " + plan.String() + " plan."
	default:
		return "is-less", (plan - total).String() + " less than your " + plan.String() + " plan."
	}
}

func planStatusClass(e EditForm) string { c, _ := e.PlanStatus(); return c }
func planStatusText(e EditForm) string  { _, t := e.PlanStatus(); return t }

// Total is what the edited month adds up to.
func (e EditForm) Total() debt.Money {
	var t debt.Money
	for _, r := range e.Rows {
		t += r.rowTotal()
	}
	return t
}

func choiceName(r EditRow) string { return fmt.Sprintf("choice_%d", r.DebtID) }
func extraName(r EditRow) string  { return fmt.Sprintf("extra_%d", r.DebtID) }

// Option is one <option> in a select.
type Option struct {
	Value string
	Label string
}

// SimData is the view model for the Simulate page.
type SimData struct {
	Shell       components.Shell
	Set         bool   // plan is set up, so changes can be added to it
	Empty       bool   // no debts to simulate
	Mode        string // "one" or "monthly"
	Amount      string
	Month       string // index into Months
	Months      []Option
	Note        string
	Tip         string
	Base        SimSummary
	Changed     SimSummary
	Saved       debt.Money
	Sooner      int
	Rows        []SimRow
	Allocs      []SimAlloc // one-time payment: which accounts it goes to
	AllocAmount debt.Money
	AllocMonth  time.Time
	Total       int
	ShowAll     bool
	Error       string
}

// SimSummary is the headline of one schedule.
type SimSummary struct {
	DebtFree time.Time
	Months   int
	Interest debt.Money
}

// SimAlloc is where part of a one-time payment goes.
type SimAlloc struct {
	Name    string
	Amount  debt.Money // extra this account gets that month
	PaysOff bool       // the payment clears it
	Left    debt.Money // balance after that month
}

// SimRow is one month of the simulated schedule.
type SimRow struct {
	Month    time.Time
	Note     string
	Paid     debt.Money
	Interest debt.Money
	Left     debt.Money
	BaseLeft debt.Money
	Applies  bool
}

func monthShort(t time.Time) string { return t.Format("Jan 2006") }
func monthLong(t time.Time) string  { return t.Format("January 2006") }

// MonthParam formats a month for URLs and form values (YYYY-MM).
func MonthParam(t time.Time) string { return t.Format("2006-01") }

func planMonthURL(t time.Time, action string) templ.SafeURL {
	return templ.SafeURL(fmt.Sprintf("/plan/months/%s/%s", MonthParam(t), action))
}

func strategyText(s debt.Strategy, o debt.Order) string {
	switch s {
	case debt.Snowball:
		return "Snowball (smallest balance first)"
	case debt.MinimumOnly:
		return "Minimum (payments only)"
	case debt.Custom:
		return "Custom (" + o.Describe() + ")"
	default:
		return "Avalanche (highest interest first)"
	}
}

// typeName is the plan type as shown in the Type dropdown.
func typeName(s debt.Strategy) string {
	switch s {
	case debt.MinimumOnly:
		return "Minimum"
	default:
		return s.Label()
	}
}

// monthDetail is the small line under a month's name.
func monthDetail(m service.PlanMonth, today time.Time) string {
	switch {
	case m.Status == debt.MonthSkipped:
		return "Skipped: interest added, nothing paid"
	case m.Extra > 0:
		return "Includes " + m.Extra.String() + " one-time payment"
	case m.Edited && m.Upcoming():
		return "Edited payment"
	case len(m.PaysOff()) > 0:
		return "Pays off " + strings.Join(m.PaysOff(), ", ")
	case m.Next && !m.Month.After(today):
		return "Due now"
	case m.Next:
		return "Next to pay"
	}
	return ""
}

func statusText(m service.PlanMonth) string {
	switch {
	case m.Status == debt.MonthPaid:
		return "Paid"
	case m.Status == debt.MonthSkipped:
		return "Skipped"
	case m.Next:
		return "Current"
	case m.Edited:
		return "Edited"
	}
	return "Upcoming"
}

func statusClass(m service.PlanMonth) string {
	switch {
	case m.Status == debt.MonthPaid:
		return "is-paid"
	case m.Status == debt.MonthSkipped:
		return "is-skipped"
	case m.Next:
		return "is-next"
	}
	return "is-upcoming"
}

func roleClass(role string) string {
	switch role {
	case debt.RolePaidOff:
		return "is-paid"
	case debt.RoleExtra:
		return "is-extra"
	case debt.RoleSkipped:
		return "is-skipped"
	}
	return "is-min"
}

func moneyOrDash(m debt.Money, skipped bool) string {
	if skipped {
		return "—"
	}
	return m.String()
}

func progressPercent(p *service.TrackedPlan) int {
	if p.StartTotal <= 0 {
		return 0
	}
	pct := int(float64(p.StartTotal-p.Owed) / float64(p.StartTotal) * 100)
	return max(0, min(100, pct))
}

func widthPercent(pct int) templ.SafeCSS {
	return templ.SafeCSS(fmt.Sprintf("width:%d%%", pct))
}

func simTabURL(mode string) templ.SafeURL { return templ.SafeURL("/simulate?mode=" + mode) }

// simAllURL repeats the current simulation with every month shown.
func simAllURL(d SimData) templ.SafeURL {
	q := url.Values{"all": {"1"}, "mode": {d.Mode}, "amount": {d.Amount}, "month": {d.Month}}
	return templ.SafeURL("/simulate?" + q.Encode())
}

func today() time.Time { return service.FirstOfMonth(time.Now()) }

// minimumPart is what a debt row shows as its payment: the minimum when
// there's extra on top (the extra gets its own line), otherwise all of it.
func minimumPart(x debt.MonthDebt) debt.Money {
	if x.Extra() > 0 {
		return x.Minimum
	}
	return x.Paid
}

// baseRole is the debt row's label once the extra has its own line:
// "Minimum + extra" becomes "Minimum"; Paid off and Skipped stay.
func baseRole(x debt.MonthDebt) string {
	if x.Role == debt.RoleExtra && x.Extra() > 0 {
		return debt.RoleMinimum
	}
	return x.Role
}
