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

type fixture struct {
	plans PlanService
	debts repository.DebtRepository
	ctx   context.Context
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db, err := storage.OpenPath(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	debts := repository.NewDebtRepository(db)
	ps := NewPlanService(debts, repository.NewSettingsRepository(db), repository.NewPlanRepository(db), debt.NewCalculator()).(*planService)
	ps.now = func() time.Time { return time.Date(2026, time.November, 21, 0, 0, 0, 0, time.Local) }
	ctx := context.Background()
	for _, d := range []debt.Debt{
		{Name: "Visa", Type: debt.TypeCard, Balance: debt.FromDollars(4500), APR: 24.99, MinimumPayment: debt.FromDollars(120), DueDay: 15},
		{Name: "Store", Type: debt.TypeCard, Balance: debt.FromDollars(300), APR: 18, MinimumPayment: debt.FromDollars(35), DueDay: 28},
	} {
		if _, err := debts.Create(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	return fixture{plans: ps, debts: debts, ctx: ctx}
}

// balances is what's left on each debt, worked out from the plan.
func (f fixture) balances(t *testing.T) map[string]debt.Money {
	t.Helper()
	left, err := f.plans.Remaining(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]debt.Money{}
	for _, d := range left {
		out[d.Name] = d.Balance
	}
	return out
}

func TestTrackedPlanLifecycle(t *testing.T) {
	f := newFixture(t)
	sep := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.Local)

	tp, err := f.plans.Tracked(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tp.Set {
		t.Fatal("plan should not be set up yet")
	}

	if err := f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, debt.FromDollars(500), 0, sep); err != nil {
		t.Fatal(err)
	}
	tp, _ = f.plans.Tracked(f.ctx)
	next := tp.Next()
	if !tp.Set || next == nil || !next.Month.Equal(sep) || tp.StartTotal != debt.FromDollars(4800) {
		t.Fatalf("unexpected plan after setup: set=%v next=%+v startTotal=%s", tp.Set, next, tp.StartTotal)
	}
	planned := next.Paid

	// Months must be marked in order.
	if err := f.plans.Mark(f.ctx, sep.AddDate(0, 1, 0), debt.MonthPaid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("marking October first: want ErrInvalid, got %v", err)
	}

	// Paid lowers balances by payment minus interest.
	before := f.balances(t)
	if err := f.plans.Mark(f.ctx, sep, debt.MonthPaid); err != nil {
		t.Fatal(err)
	}
	after := f.balances(t)
	tp, _ = f.plans.Tracked(f.ctx)
	if tp.PaidSoFar != planned || tp.PaidCount != 1 {
		t.Fatalf("paid so far %s (count %d), want %s", tp.PaidSoFar, tp.PaidCount, planned)
	}
	sep0 := tp.Months[0]
	var paidTotal debt.Money
	for _, d := range sep0.Debts {
		if after[d.Name] != d.Before+d.Interest-d.Paid || before[d.Name] != d.Before {
			t.Errorf("%s: before %s after %s, snapshot %+v", d.Name, before[d.Name], after[d.Name], d)
		}
		paidTotal += d.Paid
	}
	if paidTotal != planned {
		t.Errorf("snapshot paid %s, want %s", paidTotal, planned)
	}
	if !tp.Next().Month.Equal(sep.AddDate(0, 1, 0)) {
		t.Errorf("next month = %s, want October", tp.Next().Month)
	}

	// Skip adds interest and pays nothing.
	oct := sep.AddDate(0, 1, 0)
	beforeSkip := f.balances(t)
	if err := f.plans.Mark(f.ctx, oct, debt.MonthSkipped); err != nil {
		t.Fatal(err)
	}
	afterSkip := f.balances(t)
	if afterSkip["Visa"] <= beforeSkip["Visa"] {
		t.Errorf("skip should add interest: %s -> %s", beforeSkip["Visa"], afterSkip["Visa"])
	}

	// Only the most recent month can be undone; undo restores balances.
	if err := f.plans.Undo(f.ctx, sep); !errors.Is(err, ErrInvalid) {
		t.Fatalf("undo September: want ErrInvalid, got %v", err)
	}
	if err := f.plans.Undo(f.ctx, oct); err != nil {
		t.Fatal(err)
	}
	if got := f.balances(t); got["Visa"] != beforeSkip["Visa"] || got["Store"] != beforeSkip["Store"] {
		t.Errorf("undo didn't restore balances: %v, want %v", got, beforeSkip)
	}
}

func TestOverrideChangesProjection(t *testing.T) {
	f := newFixture(t)
	nov := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.Local)
	if err := f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, debt.FromDollars(500), 0, nov); err != nil {
		t.Fatal(err)
	}
	base, _ := f.plans.Tracked(f.ctx)

	amount := debt.FromDollars(2000)
	if err := f.plans.SetOverride(f.ctx, nov, &amount); err != nil {
		t.Fatal(err)
	}
	edited, _ := f.plans.Tracked(f.ctx)
	if !edited.Months[0].Edited || edited.Months[0].Paid != amount {
		t.Fatalf("first month = %+v, want edited with %s", edited.Months[0], amount)
	}
	if len(edited.Months) >= len(base.Months) {
		t.Errorf("a bigger payment should shorten the plan: %d vs %d months", len(edited.Months), len(base.Months))
	}
	// Highest interest first: the extra goes to Visa (24.99%), Store gets its minimum.
	roles := map[string]string{}
	for _, d := range edited.Months[0].Debts {
		roles[d.Name] = d.Role
	}
	if roles["Visa"] != debt.RoleExtra || roles["Store"] != debt.RoleMinimum {
		t.Errorf("roles = %v, want Visa extra and Store minimum", roles)
	}

	if err := f.plans.SetOverride(f.ctx, nov, nil); err != nil {
		t.Fatal(err)
	}
	reset, _ := f.plans.Tracked(f.ctx)
	if reset.Months[0].Edited || len(reset.Months) != len(base.Months) {
		t.Errorf("reset should restore the plan")
	}
}

func TestPaidOffDebtLeavesPlan(t *testing.T) {
	f := newFixture(t)
	nov := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.Local)
	if err := f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, debt.FromDollars(500), 0, nov); err != nil {
		t.Fatal(err)
	}
	amount := debt.FromDollars(5000)
	if err := f.plans.SetOverride(f.ctx, nov, &amount); err != nil {
		t.Fatal(err)
	}
	if err := f.plans.Mark(f.ctx, nov, debt.MonthPaid); err != nil {
		t.Fatal(err)
	}
	if got := f.balances(t); got["Store"] != 0 || got["Visa"] != 0 {
		t.Fatalf("both debts should be paid off, got %v", got)
	}
	tp, err := f.plans.Tracked(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tp.Next() != nil || tp.Owed != 0 || tp.Error != "" {
		t.Errorf("plan should be finished: next=%v owed=%s err=%q", tp.Next(), tp.Owed, tp.Error)
	}
}

func TestOneTimePaymentAddAndRemove(t *testing.T) {
	f := newFixture(t)
	nov := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.Local)
	dec := nov.AddDate(0, 1, 0)
	if err := f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, debt.FromDollars(500), 0, nov); err != nil {
		t.Fatal(err)
	}
	base, _ := f.plans.Tracked(f.ctx)

	if err := f.plans.AddExtra(f.ctx, dec, debt.FromDollars(1000)); err != nil {
		t.Fatal(err)
	}
	tp, _ := f.plans.Tracked(f.ctx)
	if tp.Months[1].Extra != debt.FromDollars(1000) || tp.Months[1].Paid != debt.FromDollars(1500) {
		t.Fatalf("December = %+v, want $1,000 extra and $1,500 paid", tp.Months[1])
	}
	if tp.Months[0].Paid != debt.FromDollars(500) || tp.Months[0].Extra != 0 {
		t.Errorf("November should be unchanged: %+v", tp.Months[0])
	}
	if planned, _ := f.plans.BudgetFor(f.ctx, dec); planned != debt.FromDollars(500) {
		t.Errorf("planned base for December = %s, want $500 (one-time payment kept separate)", planned)
	}

	if err := f.plans.RemoveExtra(f.ctx, dec); err != nil {
		t.Fatal(err)
	}
	tp, _ = f.plans.Tracked(f.ctx)
	if tp.Months[1].Extra != 0 || len(tp.Months) != len(base.Months) {
		t.Errorf("removing the one-time payment should restore the plan")
	}
}

func TestRaiseMonthlyOnlyAffectsFutureMonths(t *testing.T) {
	f := newFixture(t)
	nov := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.Local)
	if err := f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, debt.FromDollars(500), 0, nov); err != nil {
		t.Fatal(err)
	}
	if err := f.plans.Mark(f.ctx, nov, debt.MonthPaid); err != nil {
		t.Fatal(err)
	}
	dec := nov.AddDate(0, 1, 0)

	// From the next unpaid month (December): the plan setup changes.
	if err := f.plans.RaiseMonthly(f.ctx, dec, debt.FromDollars(100)); err != nil {
		t.Fatal(err)
	}
	tp, _ := f.plans.Tracked(f.ctx)
	if tp.MonthlyBudget != debt.FromDollars(600) || len(tp.Changes) != 0 {
		t.Fatalf("setup amount %s, changes %v; want $600 and none scheduled", tp.MonthlyBudget, tp.Changes)
	}
	if tp.Months[0].Paid != debt.FromDollars(500) {
		t.Errorf("paid November changed to %s", tp.Months[0].Paid)
	}
	if tp.Months[1].Paid != debt.FromDollars(600) {
		t.Errorf("December = %s, want $600", tp.Months[1].Paid)
	}

	// From a later month (March): scheduled; Dec-Feb keep $600.
	mar := time.Date(2027, time.March, 1, 0, 0, 0, 0, time.Local)
	if err := f.plans.RaiseMonthly(f.ctx, mar, debt.FromDollars(200)); err != nil {
		t.Fatal(err)
	}
	tp, _ = f.plans.Tracked(f.ctx)
	if tp.MonthlyBudget != debt.FromDollars(600) || len(tp.Changes) != 1 || tp.Changes[0].Amount != debt.FromDollars(800) {
		t.Fatalf("setup %s, changes %+v; want $600 now and $800 from March", tp.MonthlyBudget, tp.Changes)
	}
	for _, m := range tp.Months {
		want := debt.FromDollars(600)
		if !m.Month.Before(mar) {
			want = debt.FromDollars(800)
		}
		if m.Upcoming() && m.Paid != want && m.Paid > 0 && m.Left > 0 {
			t.Errorf("%s paid %s, want %s", m.Month.Format("Jan 2006"), m.Paid, want)
		}
	}
}

func TestReplayFromStartingAmount(t *testing.T) {
	f := newFixture(t)
	nov := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.Local)
	if err := f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, debt.FromDollars(500), 0, nov); err != nil {
		t.Fatal(err)
	}
	if err := f.plans.Mark(f.ctx, nov, debt.MonthPaid); err != nil {
		t.Fatal(err)
	}
	stored, _ := f.debts.List(f.ctx)
	for _, d := range stored {
		if d.Name == "Visa" && d.Balance != debt.FromDollars(4500) {
			t.Fatalf("marking paid changed the stored debt to %s", d.Balance)
		}
	}
	left := f.balances(t)
	if left["Visa"] >= debt.FromDollars(4500) {
		t.Fatalf("what's left on Visa = %s, want less than $4,500", left["Visa"])
	}

	// Correct Visa's starting amount: what's left follows.
	var visa debt.Debt
	for _, d := range stored {
		if d.Name == "Visa" {
			visa = d
		}
	}
	before := left["Visa"]
	visa.Balance = debt.FromDollars(5000)
	if err := f.debts.Update(f.ctx, visa); err != nil {
		t.Fatal(err)
	}
	if got := f.balances(t)["Visa"]; got <= before {
		t.Errorf("raising the starting amount should raise what's left: %s -> %s", before, got)
	}

	// A starting amount dated after November ignores November's payment.
	visa.StartDate = time.Date(2026, time.December, 3, 0, 0, 0, 0, time.Local)
	if err := f.debts.Update(f.ctx, visa); err != nil {
		t.Fatal(err)
	}
	if got := f.balances(t)["Visa"]; got != debt.FromDollars(5000) {
		t.Errorf("as of December 3, November shouldn't count: left %s, want $5,000.00", got)
	}
}

func TestReplayPaysPromosEarliestFirst(t *testing.T) {
	apr := time.Date(2029, time.April, 13, 0, 0, 0, 0, time.Local)
	jun := time.Date(2029, time.June, 13, 0, 0, 0, 0, time.Local)
	debts := []debt.Debt{{ID: 7, Name: "Store", Type: debt.TypePromo, Balance: debt.FromDollars(1836.50),
		Promos: []debt.PromoItem{{Remaining: debt.FromDollars(1409.38), Ends: apr}, {Remaining: debt.FromDollars(427.12), Ends: jun}}}}
	marked := []debt.MonthRecord{{Month: time.Date(2026, time.December, 1, 0, 0, 0, 0, time.Local), Status: debt.MonthPaid,
		Debts: []debt.MonthDebt{{DebtID: 7, Name: "Store", Paid: debt.FromDollars(1500), Role: debt.RoleExtra}}}}
	left, history := replay(debts, marked)
	got := left[7]
	if got.Balance != debt.FromDollars(336.50) || got.Promos[0].Remaining != 0 || got.Promos[1].Remaining != debt.FromDollars(336.50) {
		t.Fatalf("after $1,500: balance %s, promos %+v", got.Balance, got.Promos)
	}
	if history[0][0].Left != debt.FromDollars(336.50) {
		t.Errorf("history row = %+v", history[0][0])
	}
}

func TestInactiveDebtLeftOutAndExtraStaysOnTop(t *testing.T) {
	f := newFixture(t)
	nov := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.Local)
	if err := f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, 0, debt.FromDollars(100), nov); err != nil {
		t.Fatal(err)
	}
	tp, err := f.plans.Tracked(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := debt.FromDollars(255); tp.MonthlyBudget != want { // 120 + 35 + 100
		t.Fatalf("monthly = %s, want %s", tp.MonthlyBudget, want)
	}

	all, err := f.debts.List(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range all {
		if d.Name == "Store" {
			if err := f.debts.SetInactive(f.ctx, d.ID, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	if tp, err = f.plans.Tracked(f.ctx); err != nil {
		t.Fatal(err)
	}
	if want := debt.FromDollars(220); tp.MonthlyBudget != want { // 120 + 100: the extra stays $100
		t.Fatalf("monthly after deactivating = %s, want %s", tp.MonthlyBudget, want)
	}
	if tp.Owed != debt.FromDollars(4500) {
		t.Errorf("owed = %s, want only the active Visa", tp.Owed)
	}
	if tp.StartTotal != debt.FromDollars(4500) {
		t.Errorf("start total = %s, want only the active Visa ($4,500.00)", tp.StartTotal)
	}
	for _, m := range tp.Months {
		for _, md := range m.Debts {
			if md.Name == "Store" {
				t.Fatalf("%s pays the inactive Store debt", m.Month.Format("Jan 2006"))
			}
		}
	}
}

func TestReactivatedDebtCountsMonthsMarkedWhileInactive(t *testing.T) {
	f := newFixture(t)
	nov := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.Local)
	if err := f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, 0, debt.FromDollars(100), nov); err != nil {
		t.Fatal(err)
	}
	var store debt.Debt
	all, _ := f.debts.List(f.ctx)
	for _, d := range all {
		if d.Name == "Store" {
			store = d
		}
	}
	if err := f.debts.SetInactive(f.ctx, store.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.plans.Mark(f.ctx, nov, debt.MonthPaid); err != nil {
		t.Fatal(err)
	}
	// While inactive, Store is left alone.
	if got := f.balances(t)["Store"]; got != debt.FromDollars(300) {
		t.Fatalf("inactive Store = %s, want $300.00 untouched", got)
	}

	if err := f.debts.SetInactive(f.ctx, store.ID, false); err != nil {
		t.Fatal(err)
	}
	// Back in: November counts its minimum. 300 + 4.50 interest - 35 = 269.50.
	if got := f.balances(t)["Store"]; got != debt.FromDollars(269.50) {
		t.Fatalf("reactivated Store = %s, want $269.50", got)
	}
	tp, err := f.plans.Tracked(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var row *debt.MonthDebt
	for i, d := range tp.Months[0].Debts {
		if d.Name == "Store" {
			row = &tp.Months[0].Debts[i]
		}
	}
	if row == nil || row.Paid != debt.FromDollars(35) || row.Role != debt.RoleMinimum {
		t.Fatalf("November should show Store paying its $35 minimum, got %+v", row)
	}
}

func TestInterestPaidAndAhead(t *testing.T) {
	f := newFixture(t)
	nov := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.Local)
	if err := f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, debt.FromDollars(500), 0, nov); err != nil {
		t.Fatal(err)
	}
	before, err := f.plans.Tracked(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before.InterestPaid() != 0 || before.InterestAhead() <= 0 {
		t.Fatalf("before marking: paid %s, ahead %s", before.InterestPaid(), before.InterestAhead())
	}
	if err := f.plans.Mark(f.ctx, nov, debt.MonthPaid); err != nil {
		t.Fatal(err)
	}
	tp, err := f.plans.Tracked(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Visa 4,500 at 24.99% = 93.71; Store 300 at 18% = 4.50.
	if want := debt.FromDollars(98.21); tp.InterestPaid() != want {
		t.Errorf("interest paid = %s, want %s", tp.InterestPaid(), want)
	}
	if tp.InterestPaid()+tp.InterestAhead() != before.InterestAhead() {
		t.Errorf("paid %s + ahead %s should equal the plan's %s", tp.InterestPaid(), tp.InterestAhead(), before.InterestAhead())
	}
}
