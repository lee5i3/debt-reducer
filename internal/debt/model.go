// Package debt contains the debt domain model and the payoff calculator.
package debt

import "time"

// Debt is a single account to be paid off.
type Debt struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	Balance        Money   `json:"balance"`
	APR            float64 `json:"apr"` // annual percentage rate, e.g. 19.99
	MinimumPayment Money   `json:"minimumPayment"`
	DueDay         int     `json:"dueDay"` // day of month the payment is due (1-31)
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
)

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
	default:
		return string(s)
	}
}

// Valid reports whether s is a supported strategy.
func (s Strategy) Valid() bool {
	for _, v := range Strategies {
		if v == s {
			return true
		}
	}
	return false
}

// Settings are the saved plan defaults.
type Settings struct {
	// MonthlyBudget is the total available for debt each month. Zero means
	// "use the sum of minimum payments".
	MonthlyBudget Money    `json:"monthlyBudget"`
	Strategy      Strategy `json:"strategy"`
}

// PlanRequest is the input to the calculator.
type PlanRequest struct {
	Debts         []Debt
	MonthlyBudget Money // total available each month across all debts
	Strategy      Strategy
	StartDate     time.Time
}

// Payment is a single payment made to one debt.
type Payment struct {
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
