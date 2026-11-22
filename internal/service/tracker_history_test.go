package service

import (
	"testing"
	"time"

	"github.com/lee5i3/debt-reducer/internal/debt"
)

// Editing a debt's minimum after a month is marked doesn't change that
// month: its payment and minimum/extra split stay as recorded.
func TestEditingMinimumKeepsMarkedMonths(t *testing.T) {
	f := newFixture(t)
	nov := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.Local)
	if err := f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, 0, debt.FromDollars(100), nov); err != nil {
		t.Fatal(err)
	}
	if err := f.plans.Mark(f.ctx, nov, debt.MonthPaid); err != nil {
		t.Fatal(err)
	}
	snapshot := func() map[string]debt.MonthDebt {
		tp, err := f.plans.Tracked(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]debt.MonthDebt{}
		for _, d := range tp.Months[0].Debts {
			out[d.Name] = d
		}
		return out
	}
	before := snapshot()

	all, err := f.debts.List(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range all {
		d.MinimumPayment += debt.FromDollars(50)
		d.APR += 5
		if err := f.debts.Update(f.ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	after := snapshot()
	for name, b := range before {
		a := after[name]
		if a.Paid != b.Paid || a.Minimum != b.Minimum || a.Interest != b.Interest || a.Extra() != b.Extra() {
			t.Errorf("%s: November changed after editing the debt: before %+v, after %+v", name, b, a)
		}
	}
	if before["Visa"].Extra() == 0 {
		t.Fatalf("fixture should give Visa an extra in November, got %+v", before["Visa"])
	}
}
