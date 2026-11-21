// Package debt contains the debt domain model and the payoff calculator.
package debt

import "time"

// Debt is a single account to be paid off.
type Debt struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	Type           string  `json:"type"`    // one of Types
	Balance        Money   `json:"balance"` // starting amount as of StartDate; see Remaining in the plan service
	APR            float64 `json:"apr"`     // annual percentage rate, e.g. 19.99
	MinimumPayment Money   `json:"minimumPayment"`
	DueDay         int     `json:"dueDay"` // day of month the payment is due (1-31); 0 = not set, treated as the 1st
	// TermEnds is when a loan is due to be paid off, or when the last of a
	// promotional account's purchases expires. Nil when it doesn't apply.
	TermEnds *time.Time `json:"termEnds,omitempty"`
	// StartDate is the date Balance (and Promos) were as of. Plan months
	// from this date's month on are applied to it to work out what's left.
	StartDate time.Time `json:"startDate"`
	// Inactive debts stay on the list but are left out of the plan and Simulate.
	Inactive bool `json:"inactive,omitempty"`
	// Promos are the purchases on a promotional purchase account, earliest
	// expiring first. The debt's balance is the sum of what remains.
	Promos []PromoItem `json:"promos,omitempty"`
}

// PromoItem is one purchase on a promotional purchase account.
type PromoItem struct {
	ID        int64     `json:"id"`
	Remaining Money     `json:"remaining"` // still to pay
	Ends      time.Time `json:"ends"`      // promotion expires
}

// PromoTotal is what remains across a debt's promotional purchases.
func (d Debt) PromoTotal() Money {
	var t Money
	for _, p := range d.Promos {
		t += p.Remaining
	}
	return t
}

// NextPromoEnd is the earliest expiration among purchases still owing.
func (d Debt) NextPromoEnd(today time.Time) *time.Time {
	var next *time.Time
	for i := range d.Promos {
		p := d.Promos[i]
		if p.Remaining > 0 && p.Ends.After(today) && (next == nil || p.Ends.Before(*next)) {
			next = &d.Promos[i].Ends
		}
	}
	return next
}

// PaymentsBefore counts the monthly due dates after today and before end.
// With no due day it counts whole months instead (see MonthsBefore).
func PaymentsBefore(end, today time.Time, dueDay int) int {
	if dueDay < 1 {
		return MonthsBefore(&end, today)
	}
	n := 0
	for k := 0; k < 1200; k++ {
		first := time.Date(today.Year(), today.Month()+time.Month(k), 1, 0, 0, 0, 0, today.Location())
		day := min(dueDay, first.AddDate(0, 1, -1).Day())
		due := time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, today.Location())
		if !due.After(today) {
			continue
		}
		if !due.Before(end) {
			break
		}
		n++
	}
	return n
}

// Debt types.
const (
	TypeCard  = "Credit Card"
	TypeLoan  = "Loan"
	TypePromo = "Promotional Purchase"
)

// Types are the kinds of debt a user can pick.
var Types = []string{TypeCard, TypeLoan, TypePromo}

// DefaultType is used when no type is given.
const DefaultType = TypeCard

// InterestFree reports whether d is a promotional purchase still in its
// no-interest period on the given day.
func (d Debt) InterestFree(today time.Time) bool {
	if d.Type != TypePromo {
		return false
	}
	if len(d.Promos) > 0 {
		return d.NextPromoEnd(today) != nil
	}
	return d.TermEnds != nil && d.TermEnds.After(today)
}

// MonthsBefore returns the whole monthly payments that fit before end
// (a partial month doesn't count). It is 0 when end is nil or not after today.
func MonthsBefore(end *time.Time, today time.Time) int {
	if end == nil || !end.After(today) {
		return 0
	}
	months := (end.Year()-today.Year())*12 + int(end.Month()-today.Month())
	if end.Day() < today.Day() {
		months--
	}
	return max(months, 0)
}

// MonthsUntil returns the whole months from today until end, rounding a
// partial month up. It is 0 when end is nil or in the past.
func MonthsUntil(end *time.Time, today time.Time) int {
	if end == nil || !end.After(today) {
		return 0
	}
	months := (end.Year()-today.Year())*12 + int(end.Month()-today.Month())
	if end.Day() > today.Day() {
		months++
	}
	return months
}

// ValidType reports whether t is one of Types.
func ValidType(t string) bool {
	for _, v := range Types {
		if v == t {
			return true
		}
	}
	return false
}

// Strategy decides which debt receives extra money each month.
type Strategy string

const (
	// Avalanche pays extra toward the highest APR first (least total interest).
	Avalanche Strategy = "avalanche"
	// Snowball pays extra toward the smallest balance first (fastest wins).
	Snowball Strategy = "snowball"
	// MinimumOnly pays only the minimums; used as a baseline.
	MinimumOnly Strategy = "minimum"
	// Custom orders debts by the user's choice (see Order).
	Custom Strategy = "custom"
)

// PlanTypes are the strategies a user can pick for their plan, in display order.
var PlanTypes = []Strategy{Avalanche, Snowball, MinimumOnly, Custom}

// What a custom plan orders debts by, and which end goes first.
const (
	OrderByInterest = "interest"
	OrderByBalance  = "balance"
	OrderHighFirst  = "high"
	OrderLowFirst   = "low"
)

// Order is the custom payoff order: extra money goes to debts sorted By
// interest rate or balance, highest or lowest First.
type Order struct {
	By    string `json:"by"`    // OrderByInterest or OrderByBalance
	First string `json:"first"` // OrderHighFirst or OrderLowFirst
}

// Normalize fills in defaults for missing or unknown values.
func (o Order) Normalize() Order {
	if o.By != OrderByBalance {
		o.By = OrderByInterest
	}
	if o.First != OrderLowFirst {
		o.First = OrderHighFirst
	}
	return o
}

// Describe is a plain-language description, e.g. "highest balance first".
func (o Order) Describe() string {
	o = o.Normalize()
	end, what := "highest", "interest"
	if o.First == OrderLowFirst {
		end = "lowest"
	}
	if o.By == OrderByBalance {
		what = "balance"
	}
	return end + " " + what + " first"
}

// Strategies lists all supported strategies in display order.
var Strategies = []Strategy{Avalanche, Snowball, MinimumOnly}

// Label is a human friendly strategy name.
func (s Strategy) Label() string {
	switch s {
	case Avalanche:
		return "Avalanche"
	case Snowball:
		return "Snowball"
	case MinimumOnly:
		return "Minimums only"
	case Custom:
		return "Custom"
	default:
		return string(s)
	}
}

// Valid reports whether s is a supported strategy.
func (s Strategy) Valid() bool {
	for _, v := range PlanTypes {
		if v == s {
			return true
		}
	}
	return false
}

// Settings are the saved plan defaults.
// Theme is the app's color scheme.
type Theme string

const (
	ThemeDark  Theme = "dark"
	ThemeLight Theme = "light"
)

// Valid reports whether t is a known theme.
func (t Theme) Valid() bool { return t == ThemeDark || t == ThemeLight }

// Preferences are display settings: they change how pages look, not the plan.
type Preferences struct {
	Theme Theme `json:"theme"`
	// ExpandCurrent opens the current month's row on the plan by default.
	ExpandCurrent bool `json:"expandCurrent"`
	// NoticeSeconds is how long a "saved" banner stays; 0 keeps it open.
	NoticeSeconds int `json:"noticeSeconds"`
}

// NoticeChoices are the auto-close times offered in Settings (0 = never).
var NoticeChoices = []int{3, 5, 10, 30, 0}

// DefaultPreferences is what a new database starts with.
var DefaultPreferences = Preferences{Theme: ThemeDark, ExpandCurrent: true, NoticeSeconds: 5}

type Settings struct {
	// PlanStart is the first month of the saved plan; nil until it is set up.
	PlanStart *time.Time `json:"planStart,omitempty"`
	// StartTotal is what was owed when the plan started (for progress).
	StartTotal Money `json:"startTotal"`
	// MonthlyBudget is the total available for debt each month. Zero means
	// "use the sum of minimum payments".
	MonthlyBudget Money `json:"monthlyBudget"`
	// Extra goes on top of the minimums when MonthlyBudget is zero.
	Extra    Money    `json:"extra"`
	Strategy Strategy `json:"strategy"`
	// Order is used when Strategy is Custom.
	Order Order `json:"order"`
}

// PlanRequest is the input to the calculator.
type PlanRequest struct {
	Debts         []Debt
	MonthlyBudget Money // total available each month across all debts
	Strategy      Strategy
	Order         Order // used when Strategy is Custom
	StartDate     time.Time
	// Adjust, when set, returns the amount available in a period instead of
	// MonthlyBudget (an edited month, or extra money in a simulation).
	Adjust func(period int, budget Money) Money
	// Allocate, when it returns amounts for a period, pays each listed debt
	// that amount (capped at its balance) in place of its minimum. Money
	// left over (a debt needing less) still goes by strategy.
	Allocate func(period int) map[int64]Money
}

// Payment is a single payment made to one debt.
type Payment struct {
	DebtID    int64     `json:"debtId"`
	Debt      string    `json:"debt"`
	Date      time.Time `json:"date"`
	Amount    Money     `json:"amount"`
	Interest  Money     `json:"interest"`
	Principal Money     `json:"principal"`
	Remaining Money     `json:"remaining"`
	PaidOff   bool      `json:"paidOff"`
}

// Month groups the payments made in one billing period: each debt's next
// due date within roughly a month of the period start.
type Month struct {
	Number           int       `json:"number"`
	Date             time.Time `json:"date"`    // earliest payment date in the period
	EndDate          time.Time `json:"endDate"` // latest payment date in the period
	Payments         []Payment `json:"payments"`
	TotalPaid        Money     `json:"totalPaid"`
	TotalInterest    Money     `json:"totalInterest"`
	RemainingBalance Money     `json:"remainingBalance"`
}

// DebtSummary describes the outcome for one debt under a plan.
type DebtSummary struct {
	Name            string    `json:"name"`
	StartingBalance Money     `json:"startingBalance"`
	APR             float64   `json:"apr"`
	PayoffOrder     int       `json:"payoffOrder"`
	PayoffDate      time.Time `json:"payoffDate"`
	Months          int       `json:"months"`
	TotalInterest   Money     `json:"totalInterest"`
	TotalPaid       Money     `json:"totalPaid"`
}

// Plan is the full payoff schedule for a strategy.
type Plan struct {
	Strategy        Strategy      `json:"strategy"`
	StartDate       time.Time     `json:"startDate"`
	DebtFreeDate    time.Time     `json:"debtFreeDate"`
	Months          int           `json:"months"`
	MonthlyBudget   Money         `json:"monthlyBudget"`
	StartingBalance Money         `json:"startingBalance"`
	TotalInterest   Money         `json:"totalInterest"`
	TotalPaid       Money         `json:"totalPaid"`
	Debts           []DebtSummary `json:"debts"`
	Schedule        []Month       `json:"schedule"`
}

// Month statuses for a tracked plan.
const (
	MonthPaid    = "paid"
	MonthSkipped = "skip"
)

// Roles describe what a debt got in a month.
const (
	RoleMinimum = "Minimum"
	RoleExtra   = "Minimum + extra"
	RolePaidOff = "Paid off"
	RoleSkipped = "Skipped"
)

// MonthRecord is a month of the saved plan that was marked paid or skipped,
// or whose payment was edited ahead of time.
type MonthRecord struct {
	Month    time.Time   // first day of the month
	Status   string      // "", MonthPaid or MonthSkipped
	Override *Money      // edited payment for the month, if any
	Extra    *Money      // one-time payment added on top, if any
	Paid     Money       // total paid (marked months)
	Interest Money       // total interest added (marked months)
	Debts    []MonthDebt // what each debt got (marked months)
}

// Marked reports whether the month was marked paid or skipped.
func (m MonthRecord) Marked() bool { return m.Status != "" }

// MonthDebt is what one debt got in one month.
type MonthDebt struct {
	DebtID   int64
	Name     string
	Before   Money // balance before the month
	Interest Money
	Paid     Money
	Minimum  Money // the part of Paid that is the debt's minimum payment
	Left     Money // balance after the month
	Role     string
}

// Extra is what was paid on top of the minimum.
func (d MonthDebt) Extra() Money { return max(d.Paid-d.Minimum, 0) }

// BudgetChange is a scheduled monthly amount that starts in Month.
type BudgetChange struct {
	Month  time.Time
	Amount Money
}
