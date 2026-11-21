package debt

import (
	"testing"
	"time"
)

func TestMinimum(t *testing.T) {
	cases := []struct {
		name  string
		terms Terms
		want  Money
	}{
		// 1% of 4,966.83 = 49.67 plus 4,966.83 × 26.65% / 12 = 110.30.
		{"card", Terms{Type: TypeCard, Balance: FromDollars(4966.83), APR: 26.65}, FromDollars(159.97)},
		{"card floor", Terms{Type: TypeCard, Balance: FromDollars(500), APR: 10}, FromDollars(25)},
		{"card tiny balance", Terms{Type: TypeCard, Balance: FromDollars(10), APR: 12}, FromDollars(10.10)},
		// Standard amortization: 14,400 at 6.9% over 56 months.
		{"loan", Terms{Type: TypeLoan, Balance: FromDollars(14400), APR: 6.9, Months: 56}, FromDollars(301.49)},
		{"loan no interest", Terms{Type: TypeLoan, Balance: FromDollars(1200), Months: 12}, FromDollars(100)},
		{"promo free", Terms{Type: TypePromo, Balance: FromDollars(2400), APR: 29.99, Months: 12, InterestFree: true}, FromDollars(200)},
		{"promo free floor", Terms{Type: TypePromo, Balance: FromDollars(120), Months: 12, InterestFree: true}, FromDollars(25)},
		{"promo charging interest", Terms{Type: TypePromo, Balance: FromDollars(4966.83), APR: 26.65}, FromDollars(159.97)},
	}
	for _, c := range cases {
		if got := c.terms.Minimum(); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestMonthsUntil(t *testing.T) {
	today := time.Date(2026, time.November, 21, 0, 0, 0, 0, time.Local)
	at := func(y int, m time.Month, d int) *time.Time {
		v := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
		return &v
	}
	cases := []struct {
		name string
		end  *time.Time
		want int
	}{
		{"none", nil, 0},
		{"past", at(2026, time.November, 1), 0},
		{"same day next year", at(2027, time.November, 21), 12},
		{"partial month rounds up", at(2026, time.December, 25), 2},
		{"less than a month", at(2026, time.December, 1), 1},
	}
	for _, c := range cases {
		if got := MonthsUntil(c.end, today); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestInterestFree(t *testing.T) {
	today := time.Date(2026, time.November, 21, 0, 0, 0, 0, time.Local)
	later := today.AddDate(0, 6, 0)
	earlier := today.AddDate(0, -1, 0)
	if !(Debt{Type: TypePromo, TermEnds: &later}).InterestFree(today) {
		t.Error("running promotion should be interest-free")
	}
	if (Debt{Type: TypePromo, TermEnds: &earlier}).InterestFree(today) {
		t.Error("ended promotion should not be interest-free")
	}
	if (Debt{Type: TypeLoan, TermEnds: &later}).InterestFree(today) {
		t.Error("a loan is never interest-free")
	}
}

func TestMonthlyInterestRoundsHalfUp(t *testing.T) {
	if got := MonthlyInterest(FromDollars(6200), 24.99); got != FromDollars(129.12) {
		t.Errorf("got %s, want $129.12", got)
	}
	if got := MonthlyInterest(FromDollars(1000), 12); got != FromDollars(10) {
		t.Errorf("got %s, want $10.00", got)
	}
}

func TestMonthsBefore(t *testing.T) {
	today := time.Date(2026, time.November, 21, 0, 0, 0, 0, time.Local)
	at := func(y int, m time.Month, d int) *time.Time {
		v := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
		return &v
	}
	cases := []struct {
		end  *time.Time
		want int
	}{
		{nil, 0},
		{at(2026, time.November, 1), 0},
		{at(2027, time.November, 21), 12},
		{at(2026, time.December, 25), 1},
		{at(2026, time.December, 10), 0},
		{at(2027, time.May, 20), 5},
	}
	for _, c := range cases {
		if got := MonthsBefore(c.end, today); got != c.want {
			t.Errorf("%v: got %d, want %d", c.end, got, c.want)
		}
	}
}

func TestPromoAccountMinimum(t *testing.T) {
	// The user's example: payments due the 6th, starting Dec 6, 2026.
	today := time.Date(2026, time.November, 21, 0, 0, 0, 0, time.Local)
	apr13 := time.Date(2029, time.April, 13, 0, 0, 0, 0, time.Local)
	jun13 := time.Date(2029, time.June, 13, 0, 0, 0, 0, time.Local)
	if got := PaymentsBefore(apr13, today, 6); got != 29 {
		t.Errorf("payments before Apr 13, 2029 = %d, want 29", got)
	}
	if got := PaymentsBefore(jun13, today, 6); got != 31 {
		t.Errorf("payments before Jun 13, 2029 = %d, want 31", got)
	}
	terms := Terms{Type: TypePromo, InterestFree: true, Balance: FromDollars(1836.50), Parts: []PromoPart{
		{Remaining: FromDollars(1409.38), Payments: 29},
		{Remaining: FromDollars(427.12), Payments: 31},
	}}
	// 1409.38/29 = 48.60 + 427.12/31 = 13.78
	if got := terms.Minimum(); got != FromDollars(62.38) {
		t.Errorf("minimum = %s, want $62.38", got)
	}
}
