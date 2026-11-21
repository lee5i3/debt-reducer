package debt

import (
	"errors"
	"testing"
)

func TestLumpSumRanksOptions(t *testing.T) {
	req := PlanRequest{Debts: sampleDebts(), MonthlyBudget: FromDollars(800), Strategy: Avalanche, StartDate: start}
	a, err := NewCalculator().LumpSum(req, FromDollars(2000))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Options) != len(req.Debts) {
		t.Fatalf("got %d options, want %d", len(a.Options), len(req.Debts))
	}
	best := a.Best()
	if !best.Recommended || best.Target != "Visa" {
		t.Fatalf("best = %s (recommended=%v), want Visa", best.Target, best.Recommended)
	}
	for i, o := range a.Options {
		if o.InterestSaved <= 0 {
			t.Errorf("%s: interest saved %s, want > 0", o.Target, o.InterestSaved)
		}
		if i > 0 && o.InterestSaved > a.Options[i-1].InterestSaved {
			t.Errorf("options not sorted by interest saved at %d", i)
		}
		if i > 0 && o.Recommended {
			t.Errorf("%s should not be recommended", o.Target)
		}
		// Every dollar is accounted for: lump + remaining payments = principal + interest.
		if got, want := o.Outcome.TotalPaid, a.Baseline.TotalPaid-o.InterestSaved; got != want {
			t.Errorf("%s: total paid %s, want %s", o.Target, got, want)
		}
	}
}

func TestLumpSumSpillsOverAndFreesMinimum(t *testing.T) {
	req := PlanRequest{Debts: sampleDebts(), MonthlyBudget: FromDollars(800), Strategy: Snowball, StartDate: start}
	a, err := NewCalculator().LumpSum(req, FromDollars(1500))
	if err != nil {
		t.Fatal(err)
	}
	var store *LumpSumOption
	for i := range a.Options {
		if a.Options[i].Target == "Store Card" {
			store = &a.Options[i]
		}
	}
	if store == nil {
		t.Fatal("missing Store Card option")
	}
	// $900 clears the store card; the other $600 goes to the highest APR (Visa).
	if len(store.Allocations) != 2 || store.Allocations[1].Debt != "Visa" || store.Allocations[1].Amount != FromDollars(600) {
		t.Fatalf("unexpected allocations %+v", store.Allocations)
	}
	if len(store.PaidOff) != 1 || store.FreedMinimum != FromDollars(35) {
		t.Fatalf("paidOff=%v freed=%s, want [Store Card] and $35", store.PaidOff, store.FreedMinimum)
	}
}

func TestLumpSumCoversEverything(t *testing.T) {
	req := PlanRequest{Debts: sampleDebts(), MonthlyBudget: FromDollars(800), Strategy: Avalanche, StartDate: start}
	a, err := NewCalculator().LumpSum(req, FromDollars(1_000_000))
	if err != nil {
		t.Fatal(err)
	}
	if a.Applied != FromDollars(13400) {
		t.Fatalf("applied %s, want total owed $13,400.00", a.Applied)
	}
	if o := a.Best(); o.Outcome.Months != 0 || o.Outcome.TotalInterest != 0 || len(o.PaidOff) != 3 {
		t.Fatalf("unexpected outcome %+v", o)
	}
}

func TestLumpSumInvalidAmount(t *testing.T) {
	req := PlanRequest{Debts: sampleDebts(), MonthlyBudget: FromDollars(800), Strategy: Avalanche, StartDate: start}
	if _, err := NewCalculator().LumpSum(req, 0); !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("want ErrInvalidAmount, got %v", err)
	}
}
