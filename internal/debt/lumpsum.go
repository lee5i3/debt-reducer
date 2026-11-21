package debt

import (
	"errors"
	"sort"
	"time"
)

// ErrInvalidAmount is returned when a one-time payment is not positive.
var ErrInvalidAmount = errors.New("one-time payment must be greater than zero")

// Outcome is the headline result of a plan.
type Outcome struct {
	DebtFreeDate  time.Time `json:"debtFreeDate"`
	Months        int       `json:"months"`
	TotalInterest Money     `json:"totalInterest"`
	TotalPaid     Money     `json:"totalPaid"`
}

func outcomeOf(p *Plan) Outcome {
	return Outcome{DebtFreeDate: p.DebtFreeDate, Months: p.Months, TotalInterest: p.TotalInterest, TotalPaid: p.TotalPaid}
}

// Allocation is the part of a one-time payment applied to one debt.
type Allocation struct {
	DebtID  int64  `json:"debtId"`
	Debt    string `json:"debt"`
	Amount  Money  `json:"amount"`
	PaidOff bool   `json:"paidOff"`
}

// LumpSumOption is the result of directing a one-time payment at one debt.
type LumpSumOption struct {
	TargetID      int64        `json:"targetId"`
	Target        string       `json:"target"`
	TargetAPR     float64      `json:"targetApr"`
	Allocations   []Allocation `json:"allocations"`
	PaidOff       []string     `json:"paidOff"`      // debts eliminated by the payment
	FreedMinimum  Money        `json:"freedMinimum"` // monthly minimums no longer owed
	Outcome       Outcome      `json:"outcome"`
	InterestSaved Money        `json:"interestSaved"`
	MonthsSaved   int          `json:"monthsSaved"`
	Recommended   bool         `json:"recommended"`
}

// LumpSumAnalysis compares where a one-time payment does the most good.
type LumpSumAnalysis struct {
	Amount        Money           `json:"amount"`
	Applied       Money           `json:"applied"` // capped at the total owed
	Strategy      Strategy        `json:"strategy"`
	MonthlyBudget Money           `json:"monthlyBudget"`
	PaymentDate   time.Time       `json:"paymentDate"`
	Baseline      Outcome         `json:"baseline"`
	Options       []LumpSumOption `json:"options"` // best first
}

// Best returns the recommended option.
func (a *LumpSumAnalysis) Best() *LumpSumOption {
	if len(a.Options) == 0 {
		return nil
	}
	return &a.Options[0]
}

// LumpSum evaluates applying a one-time payment of amount, made on
// req.StartDate, to each debt in turn. The ongoing plan (req.Strategy and
// req.MonthlyBudget) is unchanged, so minimums freed by a paid-off debt roll
// into extra payments. If amount exceeds the target's balance, the remainder
// goes to the next highest-APR debt.
//
// Options are ranked by interest saved, then months saved, then monthly
// cash freed.
func (c *Calculator) LumpSum(req PlanRequest, amount Money) (*LumpSumAnalysis, error) {
	if amount <= 0 {
		return nil, ErrInvalidAmount
	}
	base, err := c.Plan(req)
	if err != nil {
		return nil, err
	}

	var owed Money
	for _, d := range req.Debts {
		owed += d.Balance
	}

	a := &LumpSumAnalysis{
		Amount:        amount,
		Applied:       minMoney(amount, owed),
		Strategy:      req.Strategy,
		MonthlyBudget: base.MonthlyBudget,
		PaymentDate:   base.StartDate,
		Baseline:      outcomeOf(base),
	}

	for i, target := range req.Debts {
		remaining, allocs := allocate(req.Debts, i, a.Applied)

		opt := LumpSumOption{
			TargetID:    target.ID,
			Target:      target.Name,
			TargetAPR:   target.APR,
			Allocations: allocs,
			PaidOff:     []string{},
		}
		for _, al := range allocs {
			if al.PaidOff {
				opt.PaidOff = append(opt.PaidOff, al.Debt)
			}
		}
		for _, d := range req.Debts {
			if !containsName(remaining, d) {
				opt.FreedMinimum += d.MinimumPayment
			}
		}

		if len(remaining) == 0 {
			opt.Outcome = Outcome{DebtFreeDate: base.StartDate}
		} else {
			r := req
			r.Debts = remaining
			p, err := c.Plan(r)
			if err != nil {
				return nil, err
			}
			opt.Outcome = outcomeOf(p)
		}
		opt.Outcome.TotalPaid += a.Applied
		opt.InterestSaved = a.Baseline.TotalInterest - opt.Outcome.TotalInterest
		opt.MonthsSaved = a.Baseline.Months - opt.Outcome.Months
		a.Options = append(a.Options, opt)
	}

	sort.SliceStable(a.Options, func(i, j int) bool {
		x, y := a.Options[i], a.Options[j]
		if x.InterestSaved != y.InterestSaved {
			return x.InterestSaved > y.InterestSaved
		}
		if x.MonthsSaved != y.MonthsSaved {
			return x.MonthsSaved > y.MonthsSaved
		}
		return x.FreedMinimum > y.FreedMinimum
	})
	if len(a.Options) > 0 {
		a.Options[0].Recommended = true
	}
	return a, nil
}

// allocate applies amount to debts[target] first, then to the rest by
// highest APR. It returns the debts still owing and the allocations made.
func allocate(debts []Debt, target int, amount Money) ([]Debt, []Allocation) {
	order := make([]int, 0, len(debts))
	order = append(order, target)
	rest := make([]int, 0, len(debts)-1)
	for i := range debts {
		if i != target {
			rest = append(rest, i)
		}
	}
	sort.SliceStable(rest, func(a, b int) bool {
		da, db := debts[rest[a]], debts[rest[b]]
		if da.APR != db.APR {
			return da.APR > db.APR
		}
		return da.Balance < db.Balance
	})
	order = append(order, rest...)

	balances := make([]Money, len(debts))
	for i, d := range debts {
		balances[i] = d.Balance
	}
	var allocs []Allocation
	for _, i := range order {
		if amount == 0 {
			break
		}
		pay := minMoney(amount, balances[i])
		balances[i] -= pay
		amount -= pay
		allocs = append(allocs, Allocation{DebtID: debts[i].ID, Debt: debts[i].Name, Amount: pay, PaidOff: balances[i] == 0})
	}

	remaining := make([]Debt, 0, len(debts))
	for i, d := range debts {
		if balances[i] > 0 {
			d.Balance = balances[i]
			remaining = append(remaining, d)
		}
	}
	return remaining, allocs
}

func containsName(debts []Debt, d Debt) bool {
	for _, x := range debts {
		if x.ID == d.ID && x.Name == d.Name {
			return true
		}
	}
	return false
}
