package service

import (
	"context"
	"time"

	"github.com/lee5i3/debt-reducer/internal/debt"
)

// Options override the saved plan settings for one calculation.
type Options struct {
	Strategy      debt.Strategy
	MonthlyBudget debt.Money
	Extra         debt.Money // on top of the minimums when MonthlyBudget is 0
	StartDate     time.Time
}

// Result is everything needed to show a plan for the saved debts.
type Result struct {
	Debts        []debt.Debt
	Options      Options // the options actually used, after defaults
	MinimumTotal debt.Money
	Plan         *debt.Plan
	Comparison   []*debt.Plan
}

// Planner builds payoff plans from the saved debts.
type Planner interface {
	// Defaults returns the saved settings resolved into Options.
	Defaults(ctx context.Context) (Options, error)
	// Build calculates the selected plan and a comparison of all strategies.
	// Zero-valued fields in opts fall back to the saved settings.
	Build(ctx context.Context, opts Options) (*Result, error)
	// LumpSum finds the best debt to put a one-time payment toward. The
	// payment is made on opts.StartDate.
	LumpSum(ctx context.Context, amount debt.Money, opts Options) (*LumpSumResult, error)
	// PayoffAtMinimum returns how many months d takes to pay off with only
	// its minimum payment. ok is false when the minimum doesn't cover interest.
	PayoffAtMinimum(d debt.Debt) (months int, ok bool)
}

// LumpSumResult is a one-time payment analysis for the saved debts.
type LumpSumResult struct {
	Debts        []debt.Debt
	Options      Options
	MinimumTotal debt.Money
	Analysis     *debt.LumpSumAnalysis
}

type planner struct {
	debts DebtService
	calc  *debt.Calculator
}

// NewPlanner is a dig constructor for Planner.
func NewPlanner(debts DebtService, calc *debt.Calculator) Planner {
	return &planner{debts: debts, calc: calc}
}

func (p *planner) Defaults(ctx context.Context) (Options, error) {
	s, err := p.debts.Settings(ctx)
	if err != nil {
		return Options{}, err
	}
	if !s.Strategy.Valid() {
		s.Strategy = debt.Avalanche
	}
	return Options{Strategy: s.Strategy, MonthlyBudget: s.MonthlyBudget, Extra: s.Extra, StartDate: today()}, nil
}

// resolve loads the debts and fills unset options from saved settings.
func (p *planner) resolve(ctx context.Context, opts Options) (debt.PlanRequest, Options, error) {
	all, err := p.debts.List(ctx)
	if err != nil {
		return debt.PlanRequest{}, opts, err
	}
	var debts []debt.Debt // paid-off (zero balance) and inactive debts are left out
	for _, d := range all {
		if d.Balance > 0 && !d.Inactive {
			debts = append(debts, d)
		}
	}
	def, err := p.Defaults(ctx)
	if err != nil {
		return debt.PlanRequest{}, opts, err
	}
	if opts.Strategy == "" {
		opts.Strategy = def.Strategy
	}
	if opts.MonthlyBudget == 0 {
		opts.MonthlyBudget = def.MonthlyBudget
	}
	if opts.MonthlyBudget == 0 {
		opts.MonthlyBudget = debt.MinimumTotal(debts) + def.Extra
	}
	if opts.StartDate.IsZero() {
		opts.StartDate = def.StartDate
	}
	set, err := p.debts.Settings(ctx)
	if err != nil {
		return debt.PlanRequest{}, opts, err
	}
	return debt.PlanRequest{
		Debts:         debts,
		MonthlyBudget: opts.MonthlyBudget,
		Strategy:      opts.Strategy,
		Order:         set.Order.Normalize(),
		StartDate:     opts.StartDate,
	}, opts, nil
}

func (p *planner) LumpSum(ctx context.Context, amount debt.Money, opts Options) (*LumpSumResult, error) {
	req, opts, err := p.resolve(ctx, opts)
	if err != nil {
		return nil, err
	}
	res := &LumpSumResult{Debts: req.Debts, Options: opts, MinimumTotal: debt.MinimumTotal(req.Debts)}
	if len(req.Debts) == 0 || amount == 0 {
		return res, nil
	}
	if amount < 0 {
		return res, invalid(debt.ErrInvalidAmount)
	}
	res.Analysis, err = p.calc.LumpSum(req, amount)
	return res, err
}

func (p *planner) PayoffAtMinimum(d debt.Debt) (int, bool) {
	plan, err := p.calc.Plan(debt.PlanRequest{Debts: []debt.Debt{d}, Strategy: debt.MinimumOnly, StartDate: today()})
	if err != nil {
		return 0, false
	}
	return plan.Months, true
}

func (p *planner) Build(ctx context.Context, opts Options) (*Result, error) {
	req, opts, err := p.resolve(ctx, opts)
	if err != nil {
		return nil, err
	}
	res := &Result{Debts: req.Debts, Options: opts, MinimumTotal: debt.MinimumTotal(req.Debts)}
	if len(req.Debts) == 0 {
		return res, nil
	}
	if res.Plan, err = p.calc.Plan(req); err != nil {
		return res, err
	}
	if res.Comparison, err = p.calc.Compare(req); err != nil {
		return res, err
	}
	return res, nil
}

func today() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
}
