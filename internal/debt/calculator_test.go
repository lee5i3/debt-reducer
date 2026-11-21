package debt

import (
	"errors"
	"testing"
	"time"
)

var start = time.Date(2026, time.November, 21, 0, 0, 0, 0, time.Local)

func sampleDebts() []Debt {
	return []Debt{
		{Name: "Car", Balance: FromDollars(8000), APR: 6.5, MinimumPayment: FromDollars(250), DueDay: 5},
		{Name: "Visa", Balance: FromDollars(4500), APR: 24.99, MinimumPayment: FromDollars(120), DueDay: 15},
		{Name: "Store Card", Balance: FromDollars(900), APR: 18, MinimumPayment: FromDollars(35), DueDay: 28},
	}
}

func TestPlanPaysEverythingOff(t *testing.T) {
	calc := NewCalculator()
	for _, s := range Strategies {
		p, err := calc.Plan(PlanRequest{Debts: sampleDebts(), MonthlyBudget: FromDollars(800), Strategy: s, StartDate: start})
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		last := p.Schedule[len(p.Schedule)-1]
		if last.RemainingBalance != 0 {
			t.Errorf("%s: remaining balance %s, want 0", s, last.RemainingBalance)
		}
		if !last.EndDate.Equal(p.DebtFreeDate) {
			t.Errorf("%s: last period ends %s, debt free %s", s, last.EndDate, p.DebtFreeDate)
		}
		if first := p.Schedule[0]; first.Date.Before(start) {
			t.Errorf("%s: first payment %s is before start %s", s, first.Date, start)
		}
		if got, want := p.TotalPaid, p.StartingBalance+p.TotalInterest; got != want {
			t.Errorf("%s: total paid %s, want principal+interest %s", s, got, want)
		}
		for _, m := range p.Schedule {
			if m.TotalPaid > p.MonthlyBudget {
				t.Errorf("%s: month %d paid %s, over budget %s", s, m.Number, m.TotalPaid, p.MonthlyBudget)
			}
		}
	}
}

func TestAvalancheBeatsSnowballOnInterest(t *testing.T) {
	plans, err := NewCalculator().Compare(PlanRequest{Debts: sampleDebts(), MonthlyBudget: FromDollars(800), StartDate: start})
	if err != nil {
		t.Fatal(err)
	}
	byStrategy := map[Strategy]*Plan{}
	for _, p := range plans {
		byStrategy[p.Strategy] = p
	}
	av, sb, min := byStrategy[Avalanche], byStrategy[Snowball], byStrategy[MinimumOnly]
	if av.TotalInterest > sb.TotalInterest {
		t.Errorf("avalanche interest %s > snowball %s", av.TotalInterest, sb.TotalInterest)
	}
	if av.TotalInterest >= min.TotalInterest || av.Months >= min.Months {
		t.Errorf("avalanche (%s, %d mo) should beat minimums (%s, %d mo)", av.TotalInterest, av.Months, min.TotalInterest, min.Months)
	}
	if av.Debts[0].Name != "Visa" {
		t.Errorf("avalanche first payoff = %s, want Visa", av.Debts[0].Name)
	}
	if sb.Debts[0].Name != "Store Card" {
		t.Errorf("snowball first payoff = %s, want Store Card", sb.Debts[0].Name)
	}
}

func TestZeroInterestSingleDebt(t *testing.T) {
	p, err := NewCalculator().Plan(PlanRequest{
		Debts:         []Debt{{Name: "Loan", Balance: FromDollars(1000), MinimumPayment: FromDollars(100), DueDay: 1}},
		MonthlyBudget: FromDollars(250),
		Strategy:      Avalanche,
		StartDate:     start,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Months != 4 || p.TotalInterest != 0 {
		t.Fatalf("months=%d interest=%s, want 4 and $0.00", p.Months, p.TotalInterest)
	}
	// Start is Nov 21, due day 1 -> first payment Dec 1, last payment Mar 1.
	want := time.Date(2027, time.March, 1, 0, 0, 0, 0, time.Local)
	if !p.DebtFreeDate.Equal(want) {
		t.Fatalf("debt free %s, want %s", p.DebtFreeDate, want)
	}
}

func TestErrors(t *testing.T) {
	calc := NewCalculator()
	if _, err := calc.Plan(PlanRequest{Strategy: Avalanche}); !errors.Is(err, ErrNoDebts) {
		t.Errorf("want ErrNoDebts, got %v", err)
	}
	if _, err := calc.Plan(PlanRequest{Debts: sampleDebts(), MonthlyBudget: FromDollars(100), Strategy: Avalanche}); !errors.Is(err, ErrBudgetTooLow) {
		t.Errorf("want ErrBudgetTooLow, got %v", err)
	}
	neverEnds := []Debt{{Name: "Bad", Balance: FromDollars(10000), APR: 30, MinimumPayment: FromDollars(50), DueDay: 1}}
	if _, err := calc.Plan(PlanRequest{Debts: neverEnds, Strategy: MinimumOnly}); !errors.Is(err, ErrNeverPaidOff) {
		t.Errorf("want ErrNeverPaidOff, got %v", err)
	}
}

func TestDueDate(t *testing.T) {
	jan31 := time.Date(2027, time.January, 31, 0, 0, 0, 0, time.Local)
	if got := dueDate(jan31, 1, 31); got.Day() != 28 || got.Month() != time.February {
		// start is on the due day, so period 0 is Jan 31 and period 1 is Feb 28.
		t.Errorf("got %s, want Feb 28", got.Format("2006-01-02"))
	}
	if got := dueDate(start, 0, 15); got.Month() != time.December || got.Day() != 15 {
		t.Errorf("got %s, want Dec 15", got.Format("2006-01-02"))
	}
}

func TestMoneyString(t *testing.T) {
	cases := map[Money]string{0: "$0.00", 5: "$0.05", 123456789: "$1,234,567.89", -1050: "-$10.50"}
	for in, want := range cases {
		if got := in.String(); got != want {
			t.Errorf("%d: got %s want %s", in, got, want)
		}
	}
}
