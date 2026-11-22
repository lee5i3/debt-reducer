package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lee5i3/debt-reducer/internal/debt"
	"github.com/lee5i3/debt-reducer/internal/repository"
	"github.com/lee5i3/debt-reducer/internal/storage"
)

type plannerFixture struct {
	planner  Planner
	debts    DebtService
	repo     repository.DebtRepository
	settings repository.SettingsRepository
	ctx      context.Context
}

// newPlannerFixture has two active debts (Visa, Store: minimums $155), one
// inactive debt and one paid-off debt.
func newPlannerFixture(t *testing.T) plannerFixture {
	t.Helper()
	db, err := storage.OpenPath(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	repo := repository.NewDebtRepository(db)
	settings := repository.NewSettingsRepository(db)
	ds := NewDebtService(repo, settings)
	ctx := context.Background()
	start := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.Local)
	for _, d := range []debt.Debt{
		{Name: "Visa", Type: debt.TypeCard, Balance: debt.FromDollars(4500), APR: 24.99, MinimumPayment: debt.FromDollars(120), DueDay: 15, StartDate: start},
		{Name: "Store", Type: debt.TypeCard, Balance: debt.FromDollars(300), APR: 18, MinimumPayment: debt.FromDollars(35), DueDay: 28, StartDate: start},
		{Name: "Paused", Type: debt.TypeCard, Balance: debt.FromDollars(2000), APR: 10, MinimumPayment: debt.FromDollars(500), DueDay: 1, StartDate: start, Inactive: true},
		// The service rejects a zero balance, so a paid-off debt goes straight to the repository.
		{Name: "Done", Type: debt.TypeCard, Balance: 0, APR: 10, MinimumPayment: debt.FromDollars(700), DueDay: 1, StartDate: start},
	} {
		if _, err := repo.Create(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	return plannerFixture{planner: NewPlanner(ds, debt.NewCalculator()), debts: ds, repo: repo, settings: settings, ctx: ctx}
}

func names(debts []debt.Debt) map[string]bool {
	out := map[string]bool{}
	for _, d := range debts {
		out[d.Name] = true
	}
	return out
}

func TestPlannerBuildLeavesOutPaidOffAndInactive(t *testing.T) {
	f := newPlannerFixture(t)
	res, err := f.planner.Build(f.ctx, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if n := names(res.Debts); len(n) != 2 || !n["Visa"] || !n["Store"] {
		t.Fatalf("debts in plan = %v, want Visa and Store", n)
	}
	if res.MinimumTotal != debt.FromDollars(155) {
		t.Errorf("minimum total = %s, want $155 (inactive and paid-off minimums left out)", res.MinimumTotal)
	}
	if res.Plan == nil || len(res.Plan.Debts) != 2 {
		t.Fatalf("plan = %+v, want 2 debts", res.Plan)
	}
	if res.Plan.StartingBalance != debt.FromDollars(4800) {
		t.Errorf("starting balance = %s, want $4,800", res.Plan.StartingBalance)
	}
	if len(res.Comparison) == 0 {
		t.Error("comparison should list strategies")
	}
	for _, p := range res.Comparison {
		if p.StartingBalance != debt.FromDollars(4800) {
			t.Errorf("%s comparison starts at %s, want $4,800", p.Strategy, p.StartingBalance)
		}
	}
}

func TestPlannerBuildDefaults(t *testing.T) {
	f := newPlannerFixture(t)

	// No budget saved: the minimums plus the saved extra.
	if err := f.debts.SaveSettings(f.ctx, debt.Settings{Strategy: debt.Snowball, Extra: debt.FromDollars(100)}); err != nil {
		t.Fatal(err)
	}
	res, err := f.planner.Build(f.ctx, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Options.Strategy != debt.Snowball {
		t.Errorf("strategy = %s, want the saved snowball", res.Options.Strategy)
	}
	if res.Options.MonthlyBudget != debt.FromDollars(255) {
		t.Errorf("budget = %s, want minimums $155 + extra $100", res.Options.MonthlyBudget)
	}
	if !sameDay(res.Options.StartDate, today()) {
		t.Errorf("start = %s, want today", res.Options.StartDate)
	}
	if res.Plan.MonthlyBudget != debt.FromDollars(255) {
		t.Errorf("plan budget = %s, want $255", res.Plan.MonthlyBudget)
	}

	// A saved fixed total wins over minimums + extra.
	if err := f.debts.SaveSettings(f.ctx, debt.Settings{Strategy: debt.Avalanche, MonthlyBudget: debt.FromDollars(400), Extra: debt.FromDollars(100)}); err != nil {
		t.Fatal(err)
	}
	res, err = f.planner.Build(f.ctx, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Options.MonthlyBudget != debt.FromDollars(400) || res.Options.Strategy != debt.Avalanche {
		t.Errorf("options = %+v, want avalanche at $400", res.Options)
	}

	// Explicit options override the saved settings.
	start := time.Date(2027, time.February, 3, 0, 0, 0, 0, time.Local)
	res, err = f.planner.Build(f.ctx, Options{Strategy: debt.MinimumOnly, MonthlyBudget: debt.FromDollars(1000), StartDate: start})
	if err != nil {
		t.Fatal(err)
	}
	if res.Options.Strategy != debt.MinimumOnly || res.Options.MonthlyBudget != debt.FromDollars(1000) || !res.Options.StartDate.Equal(start) {
		t.Errorf("options = %+v, want the ones passed in", res.Options)
	}
	if !res.Plan.StartDate.Equal(start) {
		t.Errorf("plan starts %s, want %s", res.Plan.StartDate, start)
	}
}

func TestPlannerDefaultsFallBackToAvalanche(t *testing.T) {
	f := newPlannerFixture(t)
	// An unknown strategy can only get in below the service's validation.
	if err := f.settings.Save(f.ctx, debt.Settings{Strategy: "bogus", Extra: debt.FromDollars(5)}); err != nil {
		t.Fatal(err)
	}
	opts, err := f.planner.Defaults(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Strategy != debt.Avalanche || opts.Extra != debt.FromDollars(5) || !sameDay(opts.StartDate, today()) {
		t.Errorf("defaults = %+v, want avalanche, $5 extra, today", opts)
	}
}

func TestPlannerBuildWithNoActiveDebts(t *testing.T) {
	f := newPlannerFixture(t)
	all, _ := f.repo.List(f.ctx)
	for _, d := range all {
		if err := f.repo.SetInactive(f.ctx, d.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	res, err := f.planner.Build(f.ctx, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Debts) != 0 || res.Plan != nil || res.Comparison != nil || res.MinimumTotal != 0 {
		t.Errorf("result = %+v, want empty", res)
	}
	ls, err := f.planner.LumpSum(f.ctx, debt.FromDollars(100), Options{})
	if err != nil || ls.Analysis != nil {
		t.Errorf("lump sum with no debts: analysis %v err %v, want nil and nil", ls.Analysis, err)
	}
}

func TestPlannerBuildBudgetTooLow(t *testing.T) {
	f := newPlannerFixture(t)
	_, err := f.planner.Build(f.ctx, Options{MonthlyBudget: debt.FromDollars(100)})
	if !errors.Is(err, debt.ErrBudgetTooLow) {
		t.Errorf("budget below minimums: want ErrBudgetTooLow, got %v", err)
	}
}

func TestPlannerLumpSum(t *testing.T) {
	f := newPlannerFixture(t)

	res, err := f.planner.LumpSum(f.ctx, 0, Options{})
	if err != nil || res.Analysis != nil {
		t.Errorf("zero amount: analysis %v err %v, want nothing", res.Analysis, err)
	}
	if res.MinimumTotal != debt.FromDollars(155) || len(res.Debts) != 2 {
		t.Errorf("zero amount still reports debts: %d debts, minimums %s", len(res.Debts), res.MinimumTotal)
	}

	if _, err := f.planner.LumpSum(f.ctx, -1, Options{}); !errors.Is(err, ErrInvalid) {
		t.Errorf("negative amount: want ErrInvalid, got %v", err)
	}

	res, err = f.planner.LumpSum(f.ctx, debt.FromDollars(1000), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Analysis == nil {
		t.Fatal("want an analysis")
	}
	if n := names(res.Debts); n["Paused"] || n["Done"] {
		t.Errorf("lump sum includes inactive or paid-off debts: %v", n)
	}
	if res.Options.MonthlyBudget != debt.FromDollars(155) {
		t.Errorf("budget = %s, want minimums $155", res.Options.MonthlyBudget)
	}
}

func TestPlannerPayoffAtMinimum(t *testing.T) {
	f := newPlannerFixture(t)
	d := debt.Debt{ID: 1, Name: "Loan", Type: debt.TypeLoan, Balance: debt.FromDollars(1000), APR: 0, MinimumPayment: debt.FromDollars(100), DueDay: 1}
	months, ok := f.planner.PayoffAtMinimum(d)
	if !ok || months != 10 {
		t.Errorf("$1,000 at 0%% paying $100: %d months ok=%v, want 10", months, ok)
	}

	// $10 a month never covers 24% on $10,000 ($200 interest).
	d.Balance, d.APR, d.MinimumPayment = debt.FromDollars(10000), 24, debt.FromDollars(10)
	if _, ok := f.planner.PayoffAtMinimum(d); ok {
		t.Error("a minimum below the interest should not pay off")
	}
}
