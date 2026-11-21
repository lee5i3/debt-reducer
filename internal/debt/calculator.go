package debt

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

// DefaultMaxMonths caps a simulation at 50 years.
const DefaultMaxMonths = 600

var (
	// ErrNoDebts is returned when there is nothing to calculate.
	ErrNoDebts = errors.New("no debts added yet")
	// ErrBudgetTooLow is returned when the budget does not cover all minimums.
	ErrBudgetTooLow = errors.New("monthly budget is less than the sum of minimum payments")
	// ErrNeverPaidOff is returned when payments cannot outpace interest.
	ErrNeverPaidOff = errors.New("debts are not paid off within the maximum schedule length; payments may not cover interest")
)

// Calculator builds payoff plans.
type Calculator struct {
	MaxMonths int
}

// NewCalculator returns a Calculator with default settings.
func NewCalculator() *Calculator {
	return &Calculator{MaxMonths: DefaultMaxMonths}
}

// MinimumTotal returns the sum of all minimum payments.
func MinimumTotal(debts []Debt) Money {
	var total Money
	for _, d := range debts {
		total += d.MinimumPayment
	}
	return total
}

// Validate checks a single debt for obviously bad input.
func (d Debt) Validate() error {
	switch {
	case d.Name == "":
		return errors.New("debt name is required")
	case d.Balance <= 0:
		return fmt.Errorf("%s: balance must be greater than zero", d.Name)
	case d.APR < 0:
		return fmt.Errorf("%s: apr cannot be negative", d.Name)
	case d.MinimumPayment <= 0:
		return fmt.Errorf("%s: minimum payment must be greater than zero", d.Name)
	case d.DueDay < 1 || d.DueDay > 31:
		return fmt.Errorf("%s: due day must be between 1 and 31", d.Name)
	}
	return nil
}

type account struct {
	Debt
	index    int
	balance  Money
	interest Money
	paid     Money
	payoff   time.Time
	months   int
	order    int
}

// Plan simulates month-by-month payments starting at req.StartDate.
//
// Each period, every open debt accrues one month of interest (APR/12),
// receives its minimum payment, and any budget left over is applied to
// debts in the order chosen by the strategy. Minimums freed up by paid-off
// debts roll into the extra amount automatically because the budget stays
// constant.
func (c *Calculator) Plan(req PlanRequest) (*Plan, error) {
	if len(req.Debts) == 0 {
		return nil, ErrNoDebts
	}
	if !req.Strategy.Valid() {
		return nil, fmt.Errorf("unknown strategy %q", req.Strategy)
	}
	for _, d := range req.Debts {
		if err := d.Validate(); err != nil {
			return nil, err
		}
	}

	minTotal := MinimumTotal(req.Debts)
	budget := req.MonthlyBudget
	if budget == 0 || req.Strategy == MinimumOnly {
		budget = minTotal
	}
	if budget < minTotal {
		return nil, fmt.Errorf("%w (budget %s, minimums %s)", ErrBudgetTooLow, budget, minTotal)
	}

	start := req.StartDate
	if start.IsZero() {
		start = time.Now()
	}
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.Local)

	maxMonths := c.MaxMonths
	if maxMonths <= 0 {
		maxMonths = DefaultMaxMonths
	}

	accounts := make([]*account, len(req.Debts))
	var startingBalance Money
	for i, d := range req.Debts {
		accounts[i] = &account{Debt: d, index: i, balance: d.Balance}
		startingBalance += d.Balance
	}

	plan := &Plan{
		Strategy:        req.Strategy,
		StartDate:       start,
		MonthlyBudget:   budget,
		StartingBalance: startingBalance,
	}

	paidOffCount := 0
	for period := 0; paidOffCount < len(accounts); period++ {
		if period >= maxMonths {
			return nil, ErrNeverPaidOff
		}

		month := Month{Number: period + 1}
		payments := make(map[int]*Payment)
		remaining := budget

		// 1. Accrue interest and pay minimums.
		for _, a := range accounts {
			if a.balance == 0 {
				continue
			}
			interest := Money(math.Round(float64(a.balance) * a.APR / 100 / 12))
			a.balance += interest
			a.interest += interest

			amt := minMoney(a.MinimumPayment, a.balance)
			a.balance -= amt
			a.paid += amt
			remaining -= amt

			payments[a.index] = &Payment{
				Debt:     a.Name,
				Date:     dueDate(start, period, a.DueDay),
				Amount:   amt,
				Interest: interest,
			}
		}

		// 2. Apply extra money by strategy priority.
		if req.Strategy != MinimumOnly {
			for _, a := range prioritize(accounts, req.Strategy) {
				if remaining == 0 {
					break
				}
				amt := minMoney(remaining, a.balance)
				a.balance -= amt
				a.paid += amt
				remaining -= amt
				payments[a.index].Amount += amt
			}
		}

		// 3. Record results in original debt order.
		for _, a := range accounts {
			p, ok := payments[a.index]
			if !ok {
				continue
			}
			p.Principal = p.Amount - p.Interest
			p.Remaining = a.balance
			if a.balance == 0 {
				p.PaidOff = true
				paidOffCount++
				a.payoff = p.Date
				a.months = period + 1
				a.order = paidOffCount
			}
			month.Payments = append(month.Payments, *p)
			month.TotalPaid += p.Amount
			month.TotalInterest += p.Interest
		}
		for _, a := range accounts {
			month.RemainingBalance += a.balance
		}
		sort.SliceStable(month.Payments, func(i, j int) bool {
			return month.Payments[i].Date.Before(month.Payments[j].Date)
		})
		month.Date = month.Payments[0].Date
		month.EndDate = month.Payments[len(month.Payments)-1].Date

		plan.Schedule = append(plan.Schedule, month)
		plan.TotalPaid += month.TotalPaid
		plan.TotalInterest += month.TotalInterest
	}

	plan.Months = len(plan.Schedule)
	for _, a := range accounts {
		if a.payoff.After(plan.DebtFreeDate) {
			plan.DebtFreeDate = a.payoff
		}
		plan.Debts = append(plan.Debts, DebtSummary{
			Name:            a.Name,
			StartingBalance: a.Debt.Balance,
			APR:             a.APR,
			PayoffOrder:     a.order,
			PayoffDate:      a.payoff,
			Months:          a.months,
			TotalInterest:   a.interest,
			TotalPaid:       a.paid,
		})
	}
	sort.SliceStable(plan.Debts, func(i, j int) bool {
		return plan.Debts[i].PayoffOrder < plan.Debts[j].PayoffOrder
	})

	return plan, nil
}

// Compare runs every strategy with the same inputs.
func (c *Calculator) Compare(req PlanRequest) ([]*Plan, error) {
	plans := make([]*Plan, 0, len(Strategies))
	for _, s := range Strategies {
		r := req
		r.Strategy = s
		p, err := c.Plan(r)
		if err != nil {
			if errors.Is(err, ErrNeverPaidOff) && s == MinimumOnly {
				continue // the baseline may legitimately never finish
			}
			return nil, fmt.Errorf("%s: %w", s.Label(), err)
		}
		plans = append(plans, p)
	}
	return plans, nil
}

// prioritize returns open accounts ordered by who receives extra money first.
func prioritize(accounts []*account, s Strategy) []*account {
	open := make([]*account, 0, len(accounts))
	for _, a := range accounts {
		if a.balance > 0 {
			open = append(open, a)
		}
	}
	sort.SliceStable(open, func(i, j int) bool {
		a, b := open[i], open[j]
		switch s {
		case Snowball:
			if a.balance != b.balance {
				return a.balance < b.balance
			}
			return a.APR > b.APR
		default: // Avalanche
			if a.APR != b.APR {
				return a.APR > b.APR
			}
			return a.balance < b.balance
		}
	})
	return open
}

// dueDate returns the payment date for a debt in a period. The first
// payment is the next due date on or after start; if the due day does not
// exist in a month (e.g. the 31st) the last day of that month is used.
func dueDate(start time.Time, period, dueDay int) time.Time {
	offset := period
	if start.Day() > dueDay {
		offset++
	}
	first := time.Date(start.Year(), start.Month()+time.Month(offset), 1, 0, 0, 0, 0, start.Location())
	last := first.AddDate(0, 1, -1).Day()
	if dueDay > last {
		dueDay = last
	}
	return time.Date(first.Year(), first.Month(), dueDay, 0, 0, 0, 0, start.Location())
}
