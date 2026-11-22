package service

import (
	"errors"
	"testing"
	"time"

	"github.com/lee5i3/debt-reducer/internal/debt"
)

// The fixture (see tracker_test.go) has Visa $4,500 (min $120) and Store
// $300 (min $35): minimums $155. Today is November 21, 2026.

var (
	nov26 = month(2026, time.November)
	dec26 = month(2026, time.December)
	jan27 = month(2027, time.January)
	feb27 = month(2027, time.February)
	mar27 = month(2027, time.March)
	apr27 = month(2027, time.April)
)

func (f fixture) debtNamed(t *testing.T, name string) debt.Debt {
	t.Helper()
	all, err := f.debts.List(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range all {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("no debt named %s", name)
	return debt.Debt{}
}

func (f fixture) tracked(t *testing.T) *TrackedPlan {
	t.Helper()
	tp, err := f.plans.Tracked(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	return tp
}

func (f fixture) planFor(t *testing.T, m time.Time) debt.Money {
	t.Helper()
	v, err := f.plans.PlanFor(f.ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (f fixture) budgetFor(t *testing.T, m time.Time) debt.Money {
	t.Helper()
	v, err := f.plans.BudgetFor(f.ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (f fixture) must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestSetupValidation(t *testing.T) {
	tests := []struct {
		name     string
		strategy debt.Strategy
		budget   debt.Money
		extra    debt.Money
	}{
		{"unknown strategy", "fastest", 0, 0},
		{"empty strategy", "", 0, 0},
		{"negative extra", debt.Avalanche, 0, -1},
		{"negative budget", debt.Avalanche, -1, 0},
		{"negative budget for minimums only", debt.MinimumOnly, -1, 0},
		{"fixed budget below minimums", debt.Avalanche, cents(154.99), 0},
		{"fixed budget below minimums snowball", debt.Snowball, cents(100), 0},
		{"fixed budget below minimums custom", debt.Custom, cents(1), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			err := f.plans.Setup(f.ctx, tt.strategy, debt.Order{}, tt.budget, tt.extra, nov26)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("want ErrInvalid, got %v", err)
			}
			if f.tracked(t).Set {
				t.Error("a rejected setup should not save the plan")
			}
		})
	}

	t.Run("minimums only may have a budget below minimums", func(t *testing.T) {
		f := newFixture(t)
		f.must(t, f.plans.Setup(f.ctx, debt.MinimumOnly, debt.Order{}, cents(100), 0, nov26))
		if !f.tracked(t).Set {
			t.Error("plan should be set up")
		}
	})
	t.Run("budget exactly the minimums", func(t *testing.T) {
		f := newFixture(t)
		f.must(t, f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, cents(155), 0, nov26))
	})
}

func TestSetupFixedTotalVersusExtra(t *testing.T) {
	f := newFixture(t)
	// A fixed total drops any extra.
	f.must(t, f.plans.Setup(f.ctx, debt.Snowball, debt.Order{}, cents(500), cents(100), nov26))
	tp := f.tracked(t)
	if !tp.FixedTotal || tp.Extra != 0 || tp.MonthlyBudget != cents(500) || tp.Strategy != debt.Snowball {
		t.Fatalf("fixed: fixed=%v extra=%s monthly=%s strategy=%s", tp.FixedTotal, tp.Extra, tp.MonthlyBudget, tp.Strategy)
	}
	if tp.MinimumTotal != cents(155) {
		t.Errorf("minimum total = %s, want $155", tp.MinimumTotal)
	}

	// Minimums plus extra.
	f.must(t, f.plans.Setup(f.ctx, debt.Custom, debt.Order{By: debt.OrderByBalance, First: debt.OrderLowFirst}, 0, cents(45), nov26))
	tp = f.tracked(t)
	if tp.FixedTotal || tp.Extra != cents(45) || tp.MonthlyBudget != cents(200) {
		t.Fatalf("extra: fixed=%v extra=%s monthly=%s", tp.FixedTotal, tp.Extra, tp.MonthlyBudget)
	}
	if tp.Strategy != debt.Custom || tp.Order != (debt.Order{By: debt.OrderByBalance, First: debt.OrderLowFirst}) {
		t.Errorf("strategy %s order %+v", tp.Strategy, tp.Order)
	}
	if tp.Months[0].Paid != cents(200) {
		t.Errorf("first month pays %s, want $200", tp.Months[0].Paid)
	}
}

func TestSetupStartMonth(t *testing.T) {
	f := newFixture(t)
	// The start is stored as the first of the month.
	f.must(t, f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, cents(500), 0, time.Date(2026, time.October, 17, 0, 0, 0, 0, time.Local)))
	if tp := f.tracked(t); !tp.Start.Equal(month(2026, time.October)) || !tp.Next().Month.Equal(month(2026, time.October)) {
		t.Fatalf("start %s next %s, want October 1", tp.Start, tp.Next().Month)
	}
	// Before anything is marked the start can move.
	f.must(t, f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, cents(500), 0, nov26))
	if tp := f.tracked(t); !tp.Start.Equal(nov26) {
		t.Fatalf("start %s, want November", tp.Start)
	}
	// After a month is marked it can't; the other settings still change.
	f.must(t, f.plans.Mark(f.ctx, nov26, debt.MonthPaid))
	f.must(t, f.plans.Setup(f.ctx, debt.Snowball, debt.Order{}, cents(600), 0, mar27))
	tp := f.tracked(t)
	if !tp.Start.Equal(nov26) || !tp.Next().Month.Equal(dec26) {
		t.Errorf("start %s next %s, want November and December", tp.Start, tp.Next().Month)
	}
	if tp.Strategy != debt.Snowball || tp.MonthlyBudget != cents(600) {
		t.Errorf("strategy %s monthly %s, want snowball $600", tp.Strategy, tp.MonthlyBudget)
	}
}

func TestSetupNewAmountClearsScheduledChanges(t *testing.T) {
	f := newFixture(t)
	f.must(t, f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, cents(500), 0, nov26))
	f.must(t, f.plans.RaiseMonthly(f.ctx, mar27, cents(200)))
	if tp := f.tracked(t); len(tp.Changes) != 1 {
		t.Fatalf("changes = %+v, want one", tp.Changes)
	}

	// Same amount (only the strategy changes): the schedule stays.
	f.must(t, f.plans.Setup(f.ctx, debt.Snowball, debt.Order{}, cents(500), 0, nov26))
	if tp := f.tracked(t); len(tp.Changes) != 1 || f.planFor(t, mar27) != cents(700) {
		t.Fatalf("changes = %+v, March %s; want the schedule kept", tp.Changes, f.planFor(t, mar27))
	}

	// A new amount replaces it.
	f.must(t, f.plans.Setup(f.ctx, debt.Snowball, debt.Order{}, cents(600), 0, nov26))
	if tp := f.tracked(t); len(tp.Changes) != 0 {
		t.Fatalf("changes = %+v, want none", tp.Changes)
	}
	if got := f.planFor(t, mar27); got != cents(600) {
		t.Errorf("March = %s, want $600", got)
	}

	// Switching to minimums + extra also counts as a new amount.
	f.must(t, f.plans.RaiseMonthly(f.ctx, mar27, cents(50)))
	f.must(t, f.plans.Setup(f.ctx, debt.Snowball, debt.Order{}, 0, cents(10), nov26))
	if tp := f.tracked(t); len(tp.Changes) != 0 || f.planFor(t, mar27) != cents(165) {
		t.Errorf("changes = %+v, March %s; want none and $165", tp.Changes, f.planFor(t, mar27))
	}
}

func TestRaiseMonthlyValidation(t *testing.T) {
	f := newFixture(t)
	if err := f.plans.RaiseMonthly(f.ctx, nov26, cents(10)); !errors.Is(err, ErrInvalid) {
		t.Errorf("before setup: want ErrInvalid, got %v", err)
	}
	f.must(t, f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, 0, cents(100), nov26))
	for _, amount := range []debt.Money{0, -1} {
		if err := f.plans.RaiseMonthly(f.ctx, nov26, amount); !errors.Is(err, ErrInvalid) {
			t.Errorf("amount %s: want ErrInvalid, got %v", amount, err)
		}
	}
}

func TestRaiseMonthlyExtraModel(t *testing.T) {
	f := newFixture(t)
	f.must(t, f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, 0, cents(100), nov26))
	f.must(t, f.plans.Mark(f.ctx, nov26, debt.MonthPaid))

	// From a marked month: treated as the next unpaid month (December).
	f.must(t, f.plans.RaiseMonthly(f.ctx, nov26, cents(50)))
	tp := f.tracked(t)
	if tp.FixedTotal || tp.Extra != cents(150) || tp.MonthlyBudget != cents(305) || len(tp.Changes) != 0 {
		t.Fatalf("fixed=%v extra=%s monthly=%s changes=%v; want extra $150, monthly $305", tp.FixedTotal, tp.Extra, tp.MonthlyBudget, tp.Changes)
	}
	if tp.Months[0].Paid != cents(255) {
		t.Errorf("marked November changed to %s", tp.Months[0].Paid)
	}

	// From a later month: scheduled as a total.
	f.must(t, f.plans.RaiseMonthly(f.ctx, feb27, cents(20)))
	tp = f.tracked(t)
	if tp.Extra != cents(150) || len(tp.Changes) != 1 || !tp.Changes[0].Month.Equal(feb27) || tp.Changes[0].Amount != cents(325) {
		t.Fatalf("extra %s changes %+v; want $150 and $325 from February", tp.Extra, tp.Changes)
	}
	if f.planFor(t, jan27) != cents(305) || f.planFor(t, feb27) != cents(325) || f.planFor(t, mar27) != cents(325) {
		t.Errorf("Jan %s Feb %s Mar %s, want 305/325/325", f.planFor(t, jan27), f.planFor(t, feb27), f.planFor(t, mar27))
	}
}

func TestRaiseMonthlyFixedTotalBumpsLaterChanges(t *testing.T) {
	f := newFixture(t)
	f.must(t, f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, cents(500), 0, nov26))

	f.must(t, f.plans.RaiseMonthly(f.ctx, mar27, cents(200))) // Mar: 700
	f.must(t, f.plans.RaiseMonthly(f.ctx, jan27, cents(100))) // Jan: 600, Mar: 800
	want := map[time.Time]debt.Money{nov26: cents(500), dec26: cents(500), jan27: cents(600), feb27: cents(600), mar27: cents(800), apr27: cents(800)}
	for m, w := range want {
		if got := f.planFor(t, m); got != w {
			t.Errorf("%s = %s, want %s", m.Format("Jan 2006"), got, w)
		}
	}

	// Raising the same scheduled month again adds to it without a duplicate.
	f.must(t, f.plans.RaiseMonthly(f.ctx, jan27, cents(10))) // Jan: 610, Mar: 810
	tp := f.tracked(t)
	if len(tp.Changes) != 2 || tp.Changes[0].Amount != cents(610) || tp.Changes[1].Amount != cents(810) {
		t.Fatalf("changes = %+v, want Jan $610, Mar $810", tp.Changes)
	}

	// From the next month: the fixed total goes up and so does every change.
	f.must(t, f.plans.RaiseMonthly(f.ctx, nov26, cents(50)))
	tp = f.tracked(t)
	if !tp.FixedTotal || tp.MonthlyBudget != cents(550) || tp.Extra != 0 {
		t.Fatalf("fixed=%v monthly=%s extra=%s, want fixed $550", tp.FixedTotal, tp.MonthlyBudget, tp.Extra)
	}
	if len(tp.Changes) != 2 || tp.Changes[0].Amount != cents(660) || tp.Changes[1].Amount != cents(860) {
		t.Errorf("changes = %+v, want Jan $660, Mar $860", tp.Changes)
	}
	if !tp.Changes[0].Month.Before(tp.Changes[1].Month) {
		t.Errorf("changes not sorted: %+v", tp.Changes)
	}
}

// A scheduled raise whose month has been reached (it is now the next month
// to mark, or already marked) must keep applying after another raise.
func TestRaiseMonthlyKeepsReachedScheduledChange(t *testing.T) {
	f := newFixture(t)
	f.must(t, f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, cents(500), 0, nov26))
	f.must(t, f.plans.RaiseMonthly(f.ctx, dec26, cents(100))) // Dec on: $600
	f.must(t, f.plans.Mark(f.ctx, nov26, debt.MonthPaid))
	f.must(t, f.plans.Mark(f.ctx, dec26, debt.MonthPaid))
	if got := f.planFor(t, jan27); got != cents(600) {
		t.Fatalf("January before the second raise = %s, want $600", got)
	}

	f.must(t, f.plans.RaiseMonthly(f.ctx, mar27, cents(50)))
	if got := f.planFor(t, jan27); got != cents(600) {
		t.Errorf("January after raising March = %s, want $600 (the December raise still applies)", got)
	}
	if got := f.planFor(t, mar27); got != cents(650) {
		t.Errorf("March = %s, want $650", got)
	}
}

func TestOneTimePaymentValidation(t *testing.T) {
	f := newFixture(t)
	f.must(t, f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, cents(500), 0, nov26))
	f.must(t, f.plans.Mark(f.ctx, nov26, debt.MonthPaid))

	if err := f.plans.AddExtra(f.ctx, nov26, cents(100)); !errors.Is(err, ErrInvalid) {
		t.Errorf("AddExtra on a marked month: want ErrInvalid, got %v", err)
	}
	if err := f.plans.RemoveExtra(f.ctx, nov26); !errors.Is(err, ErrInvalid) {
		t.Errorf("RemoveExtra on a marked month: want ErrInvalid, got %v", err)
	}
	for _, amount := range []debt.Money{0, -5} {
		if err := f.plans.AddExtra(f.ctx, dec26, amount); !errors.Is(err, ErrInvalid) {
			t.Errorf("AddExtra %s: want ErrInvalid, got %v", amount, err)
		}
	}

	// Adding twice adds up; any day in the month counts as that month.
	f.must(t, f.plans.AddExtra(f.ctx, time.Date(2027, time.January, 20, 0, 0, 0, 0, time.Local), cents(100)))
	f.must(t, f.plans.AddExtra(f.ctx, jan27, cents(250)))
	tp := f.tracked(t)
	var janMonth *PlanMonth
	for i := range tp.Months {
		if tp.Months[i].Month.Equal(jan27) {
			janMonth = &tp.Months[i]
		}
	}
	if janMonth == nil || janMonth.Extra != cents(350) || janMonth.Paid != cents(850) {
		t.Fatalf("January = %+v, want $350 extra and $850 paid", janMonth)
	}
	// A one-time payment isn't part of the planned amount.
	if f.budgetFor(t, jan27) != cents(500) || f.planFor(t, jan27) != cents(500) {
		t.Errorf("January planned %s / plan %s, want $500", f.budgetFor(t, jan27), f.planFor(t, jan27))
	}
}

func TestSetOverrideValidation(t *testing.T) {
	f := newFixture(t)
	f.must(t, f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, cents(500), 0, nov26))
	f.must(t, f.plans.Mark(f.ctx, nov26, debt.MonthPaid))

	amount := cents(700)
	if err := f.plans.SetOverride(f.ctx, nov26, &amount); !errors.Is(err, ErrInvalid) {
		t.Errorf("marked month: want ErrInvalid, got %v", err)
	}
	neg := cents(-1)
	if err := f.plans.SetOverride(f.ctx, dec26, &neg); !errors.Is(err, ErrInvalid) {
		t.Errorf("negative: want ErrInvalid, got %v", err)
	}

	f.must(t, f.plans.SetOverride(f.ctx, jan27, &amount))
	if f.budgetFor(t, jan27) != cents(700) {
		t.Errorf("January planned %s, want the edited $700", f.budgetFor(t, jan27))
	}
	if f.planFor(t, jan27) != cents(500) {
		t.Errorf("January plan amount %s, want $500 (before edits)", f.planFor(t, jan27))
	}
	if f.budgetFor(t, dec26) != cents(500) {
		t.Errorf("December planned %s, want $500", f.budgetFor(t, dec26))
	}
	tp := f.tracked(t)
	if !tp.Months[2].Edited || tp.Months[1].Edited {
		t.Errorf("edited flags: Dec %v Jan %v, want only January", tp.Months[1].Edited, tp.Months[2].Edited)
	}
}

func TestSetAllocations(t *testing.T) {
	f := newFixture(t)
	f.must(t, f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, cents(500), 0, nov26))
	visa, store := f.debtNamed(t, "Visa"), f.debtNamed(t, "Store")

	if err := f.plans.SetAllocations(f.ctx, dec26, map[int64]debt.Money{visa.ID: -1}); !errors.Is(err, ErrInvalid) {
		t.Errorf("negative: want ErrInvalid, got %v", err)
	}

	// Allocations replace an edited total.
	override := cents(2000)
	f.must(t, f.plans.SetOverride(f.ctx, dec26, &override))
	f.must(t, f.plans.SetAllocations(f.ctx, dec26, map[int64]debt.Money{visa.ID: cents(300), store.ID: cents(100)}))
	if got := f.budgetFor(t, dec26); got != cents(400) {
		t.Fatalf("December planned %s, want the allocations' $400", got)
	}
	tp := f.tracked(t)
	dec := tp.Months[1]
	if !dec.Edited || dec.Paid != cents(400) {
		t.Fatalf("December = %+v, want edited and $400 paid", dec)
	}
	paid := map[string]debt.Money{}
	for _, d := range dec.Debts {
		paid[d.Name] = d.Paid
	}
	if paid["Visa"] != cents(300) || paid["Store"] != cents(100) {
		t.Errorf("December per debt = %v, want Visa $300 and Store $100", paid)
	}

	// Clearing (empty map) goes back to the plan amount, not the old edited total.
	f.must(t, f.plans.SetAllocations(f.ctx, dec26, map[int64]debt.Money{}))
	if got := f.budgetFor(t, dec26); got != cents(500) {
		t.Errorf("after clearing December planned %s, want $500", got)
	}
	if f.tracked(t).Months[1].Edited {
		t.Error("December should no longer be edited")
	}

	// Marked months can't be allocated.
	f.must(t, f.plans.Mark(f.ctx, nov26, debt.MonthPaid))
	if err := f.plans.SetAllocations(f.ctx, nov26, map[int64]debt.Money{visa.ID: cents(10)}); !errors.Is(err, ErrInvalid) {
		t.Errorf("marked month: want ErrInvalid, got %v", err)
	}
}

func TestSimulate(t *testing.T) {
	f := newFixture(t)
	f.must(t, f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, cents(500), 0, nov26))
	f.must(t, f.plans.Mark(f.ctx, nov26, debt.MonthPaid))

	var periods []int
	proj, err := f.plans.Simulate(f.ctx, func(i int) debt.Money {
		periods = append(periods, i)
		if i == 0 {
			return cents(1000)
		}
		return 0
	})
	if err != nil {
		t.Fatal(err)
	}
	if !proj.Next.Equal(dec26) {
		t.Errorf("next = %s, want December", proj.Next)
	}
	if proj.Base == nil || proj.Changed == nil {
		t.Fatal("want both schedules")
	}
	if proj.Changed.Months >= proj.Base.Months || proj.Changed.TotalInterest >= proj.Base.TotalInterest {
		t.Errorf("extra money should shorten and cheapen the plan: base %d months %s, changed %d months %s",
			proj.Base.Months, proj.Base.TotalInterest, proj.Changed.Months, proj.Changed.TotalInterest)
	}
	if proj.Base.Schedule[0].TotalPaid != cents(500) || proj.Changed.Schedule[0].TotalPaid != cents(1500) {
		t.Errorf("first month base %s changed %s, want $500 and $1,500",
			proj.Base.Schedule[0].TotalPaid, proj.Changed.Schedule[0].TotalPaid)
	}
	if len(periods) == 0 || periods[0] != 0 {
		t.Errorf("extra called with periods %v, want from 0", periods)
	}

	// Base matches the tracked projection.
	tp := f.tracked(t)
	if got := len(tp.Months) - tp.MarkedCount; got != proj.Base.Months {
		t.Errorf("tracked has %d upcoming months, base %d", got, proj.Base.Months)
	}
}

func TestSimulateNothingLeft(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"Visa", "Store"} {
		f.must(t, f.debts.SetInactive(f.ctx, f.debtNamed(t, name).ID, true))
	}
	proj, err := f.plans.Simulate(f.ctx, func(int) debt.Money { return cents(10) })
	if err != nil {
		t.Fatal(err)
	}
	if proj.Base != nil || proj.Changed != nil || !proj.Next.Equal(nov26) {
		t.Errorf("projection = %+v, want no schedules starting this month", proj)
	}
}

func TestMarkValidation(t *testing.T) {
	f := newFixture(t)
	if err := f.plans.Mark(f.ctx, nov26, debt.MonthPaid); !errors.Is(err, ErrInvalid) {
		t.Errorf("before setup: want ErrInvalid, got %v", err)
	}
	f.must(t, f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, cents(500), 0, nov26))
	if err := f.plans.Mark(f.ctx, nov26, "later"); !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown status: want ErrInvalid, got %v", err)
	}
	if err := f.plans.Mark(f.ctx, month(2026, time.October), debt.MonthPaid); !errors.Is(err, ErrInvalid) {
		t.Errorf("month before the next: want ErrInvalid, got %v", err)
	}

	// Pay everything off, then there's nothing left to mark.
	all := cents(10000)
	f.must(t, f.plans.SetOverride(f.ctx, nov26, &all))
	f.must(t, f.plans.Mark(f.ctx, nov26, debt.MonthPaid))
	if err := f.plans.Mark(f.ctx, dec26, debt.MonthPaid); !errors.Is(err, ErrInvalid) {
		t.Errorf("nothing left: want ErrInvalid, got %v", err)
	}
}

func TestSkippedMonthRecord(t *testing.T) {
	f := newFixture(t)
	f.must(t, f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, cents(500), 0, nov26))
	f.must(t, f.plans.Mark(f.ctx, nov26, debt.MonthSkipped))
	tp := f.tracked(t)
	m := tp.Months[0]
	if m.Status != debt.MonthSkipped || m.Paid != 0 || m.Upcoming() {
		t.Fatalf("November = %+v, want skipped and nothing paid", m)
	}
	// Visa 4,500 at 24.99% = 93.71; Store 300 at 18% = 4.50.
	if m.Interest != cents(98.21) || m.Left != cents(4898.21) {
		t.Errorf("interest %s left %s, want $98.21 and $4,898.21", m.Interest, m.Left)
	}
	for _, d := range m.Debts {
		if d.Role != debt.RoleSkipped || d.Paid != 0 {
			t.Errorf("%s row = %+v, want skipped", d.Name, d)
		}
	}
	if tp.PaidCount != 0 || tp.PaidSoFar != 0 || tp.MarkedCount != 1 {
		t.Errorf("paid count %d so far %s marked %d, want 0/0/1", tp.PaidCount, tp.PaidSoFar, tp.MarkedCount)
	}
	if tp.Owed != cents(4898.21) {
		t.Errorf("owed = %s, want $4,898.21", tp.Owed)
	}
}

func TestUndoOnlyLatest(t *testing.T) {
	f := newFixture(t)
	f.must(t, f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, cents(500), 0, nov26))
	if err := f.plans.Undo(f.ctx, nov26); !errors.Is(err, ErrInvalid) {
		t.Errorf("nothing marked: want ErrInvalid, got %v", err)
	}
	f.must(t, f.plans.Mark(f.ctx, nov26, debt.MonthPaid))
	f.must(t, f.plans.Mark(f.ctx, dec26, debt.MonthPaid))
	tp := f.tracked(t)
	if tp.Months[0].CanUndo || !tp.Months[1].CanUndo || tp.Months[2].CanUndo {
		t.Errorf("CanUndo = %v/%v/%v, want only December", tp.Months[0].CanUndo, tp.Months[1].CanUndo, tp.Months[2].CanUndo)
	}
	if err := f.plans.Undo(f.ctx, nov26); !errors.Is(err, ErrInvalid) {
		t.Errorf("undo November: want ErrInvalid, got %v", err)
	}
	if err := f.plans.Undo(f.ctx, jan27); !errors.Is(err, ErrInvalid) {
		t.Errorf("undo an unmarked month: want ErrInvalid, got %v", err)
	}
	// Any day in the month works.
	f.must(t, f.plans.Undo(f.ctx, time.Date(2026, time.December, 25, 0, 0, 0, 0, time.Local)))
	f.must(t, f.plans.Undo(f.ctx, nov26))
	tp = f.tracked(t)
	if tp.MarkedCount != 0 || !tp.Next().Month.Equal(nov26) {
		t.Errorf("after undoing both: marked %d next %s", tp.MarkedCount, tp.Next().Month)
	}
	if b := f.balances(t); b["Visa"] != cents(4500) || b["Store"] != cents(300) {
		t.Errorf("balances = %v, want the starting amounts", b)
	}
}

func TestStartTotal(t *testing.T) {
	t.Run("no months marked is what's owed by active debts", func(t *testing.T) {
		f := newFixture(t)
		f.must(t, f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, cents(500), 0, nov26))
		if got := f.tracked(t).StartTotal; got != cents(4800) {
			t.Errorf("start total = %s, want $4,800", got)
		}
	})

	t.Run("uses each debt's first applied month", func(t *testing.T) {
		f := newFixture(t)
		f.must(t, f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, cents(500), 0, nov26))
		f.must(t, f.plans.Mark(f.ctx, nov26, debt.MonthPaid))
		f.must(t, f.plans.Mark(f.ctx, dec26, debt.MonthPaid))
		if got := f.tracked(t).StartTotal; got != cents(4800) {
			t.Fatalf("start total = %s, want $4,800 (going into November)", got)
		}

		// Visa is now $5,000 as of December 3: November doesn't apply to
		// it, so its first applied month is December.
		visa := f.debtNamed(t, "Visa")
		visa.Balance = cents(5000)
		visa.StartDate = time.Date(2026, time.December, 3, 0, 0, 0, 0, time.Local)
		f.must(t, f.debts.Update(f.ctx, visa))
		tp := f.tracked(t)
		if tp.StartTotal != cents(5300) {
			t.Errorf("start total = %s, want Visa $5,000 + Store $300", tp.StartTotal)
		}
		// The November row is still shown as recorded.
		for _, d := range tp.Months[0].Debts {
			if d.Name == "Visa" && d.Before != cents(4500) {
				t.Errorf("November Visa row = %+v, want the recorded $4,500", d)
			}
		}
	})

	t.Run("inactive debts don't count and untouched debts count what's left", func(t *testing.T) {
		f := newFixture(t)
		f.must(t, f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, cents(500), 0, nov26))
		f.must(t, f.plans.Mark(f.ctx, nov26, debt.MonthPaid))
		f.must(t, f.debts.SetInactive(f.ctx, f.debtNamed(t, "Store").ID, true))
		// Added after November with a December start: no month touched it.
		if _, err := f.debts.Create(f.ctx, debt.Debt{Name: "Car", Type: debt.TypeLoan, Balance: cents(1000), APR: 5,
			MinimumPayment: cents(100), StartDate: time.Date(2026, time.December, 2, 0, 0, 0, 0, time.Local)}); err != nil {
			t.Fatal(err)
		}
		tp := f.tracked(t)
		if tp.StartTotal != cents(5500) {
			t.Errorf("start total = %s, want Visa $4,500 + Car $1,000", tp.StartTotal)
		}
		if tp.MinimumTotal != cents(220) {
			t.Errorf("minimum total = %s, want Visa $120 + Car $100", tp.MinimumTotal)
		}
	})
}

func TestTrackedProjectionError(t *testing.T) {
	f := newFixture(t)
	f.must(t, f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, cents(500), 0, nov26))
	// A new debt pushes the minimums above the fixed $500.
	if _, err := f.debts.Create(f.ctx, debt.Debt{Name: "Car", Type: debt.TypeLoan, Balance: cents(10000), APR: 5,
		MinimumPayment: cents(400), StartDate: nov26}); err != nil {
		t.Fatal(err)
	}
	tp, err := f.plans.Tracked(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tp.Error == "" || len(tp.Months) != 0 {
		t.Errorf("error %q months %d, want an error and no projection", tp.Error, len(tp.Months))
	}
	if !errors.Is(f.plans.Mark(f.ctx, nov26, debt.MonthPaid), debt.ErrBudgetTooLow) {
		t.Error("marking paid with a budget below minimums should fail with ErrBudgetTooLow")
	}
	if _, err := f.plans.Simulate(f.ctx, nil); !errors.Is(err, debt.ErrBudgetTooLow) {
		t.Errorf("Simulate: want ErrBudgetTooLow, got %v", err)
	}
}

func TestTrackedPlanHelpers(t *testing.T) {
	empty := &TrackedPlan{}
	if empty.Next() != nil || !empty.DebtFree().IsZero() || empty.MonthsLeft() != 0 || empty.InterestPaid() != 0 || empty.InterestAhead() != 0 {
		t.Error("an empty plan should have no next month, debt-free date, months or interest")
	}

	tp := &TrackedPlan{MarkedCount: 1, Months: []PlanMonth{
		{Month: nov26, Status: debt.MonthPaid, Interest: cents(10)},
		{Month: dec26, Next: true, Interest: cents(7), Debts: []debt.MonthDebt{{Name: "Store", Role: debt.RolePaidOff}, {Name: "Visa", Role: debt.RoleExtra}}},
		{Month: jan27, Interest: cents(3), Debts: []debt.MonthDebt{{Name: "Visa", Role: debt.RolePaidOff}}},
	}}
	if n := tp.Next(); n == nil || !n.Month.Equal(dec26) {
		t.Errorf("next = %v, want December", n)
	}
	if !tp.DebtFree().Equal(jan27) || tp.MonthsLeft() != 2 {
		t.Errorf("debt free %s months left %d, want January and 2", tp.DebtFree(), tp.MonthsLeft())
	}
	if tp.InterestPaid() != cents(10) || tp.InterestAhead() != cents(10) {
		t.Errorf("interest paid %s ahead %s, want $10 and $10", tp.InterestPaid(), tp.InterestAhead())
	}
	if got := tp.Months[1].PaysOff(); len(got) != 1 || got[0] != "Store" {
		t.Errorf("December pays off %v, want [Store]", got)
	}
	if got := tp.Months[0].PaysOff(); got != nil {
		t.Errorf("November pays off %v, want none", got)
	}
	if tp.Months[0].Upcoming() || !tp.Months[1].Upcoming() {
		t.Error("Upcoming should be false for marked months and true otherwise")
	}
}

func TestTrackedProjectionRolesAndTotals(t *testing.T) {
	f := newFixture(t)
	f.must(t, f.plans.Setup(f.ctx, debt.Avalanche, debt.Order{}, cents(500), 0, nov26))
	tp := f.tracked(t)
	if tp.Owed != cents(4800) || tp.MarkedCount != 0 || tp.MonthsLeft() != len(tp.Months) {
		t.Errorf("owed %s marked %d left %d of %d", tp.Owed, tp.MarkedCount, tp.MonthsLeft(), len(tp.Months))
	}
	last := tp.Months[len(tp.Months)-1]
	if last.Left != 0 || !tp.DebtFree().Equal(last.Month) {
		t.Errorf("last month %+v should leave nothing owed", last)
	}
	paidOff := map[string]bool{}
	for _, m := range tp.Months {
		var paid, interest debt.Money
		for _, d := range m.Debts {
			paid += d.Paid
			interest += d.Interest
			if d.Before+d.Interest-d.Paid != d.Left {
				t.Errorf("%s %s: before %s + interest %s - paid %s != left %s", m.Month.Format("Jan 2006"), d.Name, d.Before, d.Interest, d.Paid, d.Left)
			}
			if d.Minimum > d.Paid {
				t.Errorf("%s %s: minimum %s above paid %s", m.Month.Format("Jan 2006"), d.Name, d.Minimum, d.Paid)
			}
		}
		if paid != m.Paid || interest != m.Interest {
			t.Errorf("%s: rows paid %s interest %s, month says %s / %s", m.Month.Format("Jan 2006"), paid, interest, m.Paid, m.Interest)
		}
		for _, n := range m.PaysOff() {
			paidOff[n] = true
		}
	}
	if !paidOff["Visa"] || !paidOff["Store"] {
		t.Errorf("paid off over the plan: %v, want both", paidOff)
	}
}
