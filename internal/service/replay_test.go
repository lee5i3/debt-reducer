package service

import (
	"testing"
	"time"

	"github.com/lee5i3/debt-reducer/internal/debt"
)

func month(y int, m time.Month) time.Time { return time.Date(y, m, 1, 0, 0, 0, 0, time.Local) }

func cents(d float64) debt.Money { return debt.FromDollars(d) }

// card is $1,000 at 12% (interest $10 a month) with a $50 minimum.
func card() debt.Debt {
	return debt.Debt{ID: 1, Name: "Card", Type: debt.TypeCard, Balance: cents(1000), APR: 12,
		MinimumPayment: cents(50), StartDate: time.Date(2026, time.November, 15, 0, 0, 0, 0, time.Local)}
}

// applyRecorded uses the payment, interest and minimum recorded for the
// month, whatever the debt's APR or minimum is now.
func TestApplyRecorded(t *testing.T) {
	rec := func(interest, paid, minimum float64, role string) debt.MonthDebt {
		return debt.MonthDebt{Interest: cents(interest), Paid: cents(paid), Minimum: cents(minimum), Role: role}
	}
	tests := []struct {
		name   string
		debt   func() debt.Debt
		status string
		row    debt.MonthDebt
		want   debt.MonthDebt
	}{
		{
			name: "minimum payment", debt: card, status: debt.MonthPaid, row: rec(10, 50, 50, debt.RoleMinimum),
			want: debt.MonthDebt{Before: cents(1000), Interest: cents(10), Paid: cents(50), Minimum: cents(50), Left: cents(960), Role: debt.RoleMinimum},
		},
		{
			name: "minimum plus extra", debt: card, status: debt.MonthPaid, row: rec(10, 200, 50, debt.RoleExtra),
			want: debt.MonthDebt{Before: cents(1000), Interest: cents(10), Paid: cents(200), Minimum: cents(50), Left: cents(810), Role: debt.RoleExtra},
		},
		{
			name: "skipped adds interest and pays nothing", debt: card, status: debt.MonthSkipped, row: rec(10, 200, 50, debt.RoleSkipped),
			want: debt.MonthDebt{Before: cents(1000), Interest: cents(10), Paid: 0, Minimum: 0, Left: cents(1010), Role: debt.RoleSkipped},
		},
		{
			name: "exact payoff", debt: card, status: debt.MonthPaid, row: rec(10, 1010, 50, debt.RoleExtra),
			want: debt.MonthDebt{Before: cents(1000), Interest: cents(10), Paid: cents(1010), Minimum: cents(50), Left: 0, Role: debt.RolePaidOff},
		},
		{
			name: "more than is left (starting amount lowered) stops at zero", debt: card, status: debt.MonthPaid, row: rec(10, 1500, 50, debt.RolePaidOff),
			want: debt.MonthDebt{Before: cents(1000), Interest: cents(10), Paid: cents(1500), Minimum: cents(50), Left: 0, Role: debt.RolePaidOff},
		},
		{
			name: "recorded paid off that no longer clears", debt: card, status: debt.MonthPaid, row: rec(10, 500, 50, debt.RolePaidOff),
			want: debt.MonthDebt{Before: cents(1000), Interest: cents(10), Paid: cents(500), Minimum: cents(50), Left: cents(510), Role: debt.RoleExtra},
		},
		{
			name: "recorded interest stays after an APR change", debt: func() debt.Debt { d := card(); d.APR = 30; return d },
			status: debt.MonthPaid, row: rec(10, 50, 50, debt.RoleMinimum),
			want: debt.MonthDebt{Before: cents(1000), Interest: cents(10), Paid: cents(50), Minimum: cents(50), Left: cents(960), Role: debt.RoleMinimum},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := tt.debt()
			got, row := applyRecorded(d, tt.status, tt.row)
			tt.want.DebtID, tt.want.Name = d.ID, d.Name
			if row != tt.want {
				t.Errorf("row = %+v\nwant  %+v", row, tt.want)
			}
			if got.Balance != tt.want.Left {
				t.Errorf("balance = %s, want %s", got.Balance, tt.want.Left)
			}
		})
	}
}

func promoAccount() debt.Debt {
	return debt.Debt{ID: 2, Name: "Store", Type: debt.TypePromo, Balance: cents(500), APR: 0, MinimumPayment: cents(25),
		StartDate: month(2026, time.November),
		Promos: []debt.PromoItem{
			{ID: 1, Remaining: cents(300), Ends: time.Date(2027, time.March, 1, 0, 0, 0, 0, time.Local)},
			{ID: 2, Remaining: cents(200), Ends: time.Date(2027, time.August, 1, 0, 0, 0, 0, time.Local)},
		}}
}

func TestApplyRecordedPromos(t *testing.T) {
	paid := func(interest, amount float64) debt.MonthDebt {
		return debt.MonthDebt{Interest: cents(interest), Paid: cents(amount), Role: debt.RoleExtra}
	}
	t.Run("earliest expiring paid first", func(t *testing.T) {
		d, _ := applyRecorded(promoAccount(), debt.MonthPaid, paid(0, 350))
		if d.Promos[0].Remaining != 0 || d.Promos[1].Remaining != cents(150) || d.Balance != cents(150) {
			t.Errorf("promos %+v balance %s, want 0/150 and $150", d.Promos, d.Balance)
		}
	})
	t.Run("partial payment stays on the first purchase", func(t *testing.T) {
		d, _ := applyRecorded(promoAccount(), debt.MonthPaid, paid(0, 100))
		if d.Promos[0].Remaining != cents(200) || d.Promos[1].Remaining != cents(200) {
			t.Errorf("promos %+v, want 200/200", d.Promos)
		}
	})
	t.Run("paying it all clears every purchase", func(t *testing.T) {
		d, row := applyRecorded(promoAccount(), debt.MonthPaid, paid(0, 500))
		if d.Promos[0].Remaining != 0 || d.Promos[1].Remaining != 0 || row.Role != debt.RolePaidOff || d.Balance != 0 {
			t.Errorf("promos %+v row %+v, want both cleared and paid off", d.Promos, row)
		}
	})
	t.Run("interest goes on the last purchase", func(t *testing.T) {
		d, _ := applyRecorded(promoAccount(), debt.MonthSkipped, paid(5, 100))
		if d.Promos[0].Remaining != cents(300) || d.Promos[1].Remaining != cents(205) || d.Balance != cents(505) {
			t.Errorf("promos %+v balance %s, want 300/205 and $505", d.Promos, d.Balance)
		}
	})
	t.Run("payment that only covers the interest leaves purchases alone", func(t *testing.T) {
		d, _ := applyRecorded(promoAccount(), debt.MonthPaid, paid(5, 5))
		if d.Promos[0].Remaining != cents(300) || d.Promos[1].Remaining != cents(200) {
			t.Errorf("promos %+v, want unchanged", d.Promos)
		}
	})
	t.Run("interest is paid before principal", func(t *testing.T) {
		d, _ := applyRecorded(promoAccount(), debt.MonthPaid, paid(5, 105))
		if d.Promos[0].Remaining != cents(200) || d.Promos[1].Remaining != cents(200) {
			t.Errorf("promos %+v, want 200/200", d.Promos)
		}
	})
}

func TestReplay(t *testing.T) {
	nov, dec, jan := month(2026, time.November), month(2026, time.December), month(2027, time.January)

	t.Run("month before the start month keeps the recorded row", func(t *testing.T) {
		d := card()
		d.StartDate = time.Date(2026, time.December, 3, 0, 0, 0, 0, time.Local)
		recorded := debt.MonthDebt{DebtID: 1, Name: "Card (old)", Before: cents(1234), Interest: cents(12), Paid: cents(99), Minimum: cents(50), Left: cents(1147), Role: debt.RoleExtra}
		left, hist := replay([]debt.Debt{d}, []debt.MonthRecord{{Month: nov, Status: debt.MonthPaid, Debts: []debt.MonthDebt{recorded}}})
		if len(hist[0]) != 1 || hist[0][0] != recorded {
			t.Errorf("history = %+v, want the recorded row unchanged", hist[0])
		}
		if left[1].Balance != cents(1000) {
			t.Errorf("balance = %s, want the starting $1,000", left[1].Balance)
		}
	})

	t.Run("start date mid-month counts that month", func(t *testing.T) {
		// card() starts November 15; November is applied.
		left, hist := replay([]debt.Debt{card()}, []debt.MonthRecord{{Month: nov, Status: debt.MonthPaid,
			Debts: []debt.MonthDebt{{DebtID: 1, Interest: cents(10), Paid: cents(50), Role: debt.RoleMinimum}}}})
		if left[1].Balance != cents(960) || hist[0][0].Before != cents(1000) || hist[0][0].Name != "Card" {
			t.Errorf("balance %s row %+v, want $960 from $1,000", left[1].Balance, hist[0][0])
		}
	})

	t.Run("deleted debt keeps the recorded row", func(t *testing.T) {
		recorded := debt.MonthDebt{DebtID: 99, Name: "Gone", Before: cents(10), Paid: cents(10), Role: debt.RolePaidOff}
		left, hist := replay(nil, []debt.MonthRecord{{Month: nov, Status: debt.MonthPaid, Debts: []debt.MonthDebt{recorded}}})
		if len(left) != 0 || len(hist[0]) != 1 || hist[0][0] != recorded {
			t.Errorf("left %v history %+v", left, hist[0])
		}
	})

	t.Run("months apply in order", func(t *testing.T) {
		marked := []debt.MonthRecord{
			{Month: nov, Status: debt.MonthPaid, Debts: []debt.MonthDebt{{DebtID: 1, Interest: cents(10), Paid: cents(50), Role: debt.RoleMinimum}}},
			{Month: dec, Status: debt.MonthSkipped, Debts: []debt.MonthDebt{{DebtID: 1, Interest: cents(9.60), Role: debt.RoleSkipped}}},
			{Month: jan, Status: debt.MonthPaid, Debts: []debt.MonthDebt{{DebtID: 1, Interest: cents(9.70), Paid: cents(979.30), Role: debt.RoleExtra}}},
		}
		left, hist := replay([]debt.Debt{card()}, marked)
		// Nov: 1000 + 10 - 50 = 960. Dec: 960 + 9.60 = 969.60. Jan: 969.60 + 9.70 = 979.30, paid off.
		wantBefore := []debt.Money{cents(1000), cents(960), cents(969.60)}
		wantRole := []string{debt.RoleMinimum, debt.RoleSkipped, debt.RolePaidOff}
		for i := range marked {
			if hist[i][0].Before != wantBefore[i] || hist[i][0].Role != wantRole[i] {
				t.Errorf("month %d row = %+v, want before %s role %s", i, hist[i][0], wantBefore[i], wantRole[i])
			}
		}
		if hist[2][0].Paid != cents(979.30) || left[1].Balance != 0 {
			t.Errorf("January paid %s left %s, want $979.30 and 0", hist[2][0].Paid, left[1].Balance)
		}
	})

	// Marked months are history: a debt that wasn't in one (inactive then,
	// reactivated or added later) is never added to it.
	for _, tt := range []struct {
		name   string
		modify func(d *debt.Debt)
	}{
		{"active", func(d *debt.Debt) {}},
		{"inactive", func(d *debt.Debt) { d.Inactive = true }},
	} {
		t.Run(tt.name+" debt not marked in a month is left out of it", func(t *testing.T) {
			other := card()
			other.ID, other.Name = 2, "Other"
			tt.modify(&other)
			marked := []debt.MonthRecord{
				{Month: nov, Status: debt.MonthPaid, Debts: []debt.MonthDebt{{DebtID: 1, Interest: cents(10), Paid: cents(50), Role: debt.RoleMinimum}}},
				{Month: dec, Status: debt.MonthSkipped, Debts: []debt.MonthDebt{{DebtID: 1, Interest: cents(9.60), Role: debt.RoleSkipped}}},
			}
			left, hist := replay([]debt.Debt{card(), other}, marked)
			for i := range marked {
				if len(hist[i]) != 1 || hist[i][0].DebtID != 1 {
					t.Errorf("month %d rows = %+v, want only the marked debt", i, hist[i])
				}
			}
			if left[2].Balance != cents(1000) {
				t.Errorf("unmarked debt balance = %s, want its $1,000 untouched", left[2].Balance)
			}
		})
	}

	t.Run("input promos are not changed", func(t *testing.T) {
		p := promoAccount()
		in := []debt.Debt{p}
		left, _ := replay(in, []debt.MonthRecord{{Month: nov, Status: debt.MonthPaid,
			Debts: []debt.MonthDebt{{DebtID: 2, Paid: cents(350), Role: debt.RoleExtra}}}})
		if in[0].Promos[0].Remaining != cents(300) || in[0].Promos[1].Remaining != cents(200) {
			t.Errorf("replay changed the caller's purchases: %+v", in[0].Promos)
		}
		if left[2].Promos[0].Remaining != 0 || left[2].Promos[1].Remaining != cents(150) {
			t.Errorf("left purchases = %+v, want 0/150", left[2].Promos)
		}
	})

	t.Run("no marked months", func(t *testing.T) {
		left, hist := replay([]debt.Debt{card()}, nil)
		if len(hist) != 0 || left[1].Balance != cents(1000) {
			t.Errorf("left %+v history %v", left[1], hist)
		}
	})
}

func TestStartOrToday(t *testing.T) {
	today := time.Date(2026, time.November, 21, 0, 0, 0, 0, time.Local)
	if got := startOrToday(debt.Debt{}, today); !got.Equal(today) {
		t.Errorf("no start date: got %s, want today", got)
	}
	start := time.Date(2025, time.May, 2, 0, 0, 0, 0, time.Local)
	if got := startOrToday(debt.Debt{StartDate: start}, today); !got.Equal(start) {
		t.Errorf("got %s, want %s", got, start)
	}
}

func TestFirstOfMonth(t *testing.T) {
	got := FirstOfMonth(time.Date(2026, time.February, 28, 23, 59, 0, 0, time.Local))
	if !got.Equal(time.Date(2026, time.February, 1, 0, 0, 0, 0, time.Local)) {
		t.Errorf("FirstOfMonth = %s", got)
	}
}

// Months that were marked keep what was recorded: editing a debt's minimum
// or APR afterwards doesn't change their payment, interest or split.
func TestReplayKeepsMarkedMonthsWhenDebtIsEdited(t *testing.T) {
	nov := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.Local)
	d := debt.Debt{ID: 1, Name: "Card", Balance: cents(1000), APR: 12, MinimumPayment: cents(40),
		StartDate: time.Date(2026, time.October, 1, 0, 0, 0, 0, time.Local)}
	recorded := debt.MonthDebt{DebtID: 1, Name: "Card", Before: cents(1000), Interest: cents(10),
		Paid: cents(100), Minimum: cents(40), Left: cents(910), Role: debt.RoleExtra}
	marked := []debt.MonthRecord{{Month: nov, Status: debt.MonthPaid, Debts: []debt.MonthDebt{recorded}}}

	edited := d
	edited.MinimumPayment = cents(75) // minimum raised later
	edited.APR = 24                   // and the rate changed
	left, hist := replay([]debt.Debt{edited}, marked)
	row := hist[0][0]
	if row.Paid != cents(100) || row.Interest != cents(10) || row.Minimum != cents(40) || row.Extra() != cents(60) {
		t.Errorf("marked row changed after edit: %+v (extra %s)", row, row.Extra())
	}
	if left[1].Balance != cents(910) {
		t.Errorf("left = %s, want $910.00", left[1].Balance)
	}

	// A corrected starting amount still moves the balance, not the payment.
	edited.Balance = cents(1200)
	left, hist = replay([]debt.Debt{edited}, marked)
	if hist[0][0].Paid != cents(100) || left[1].Balance != cents(1110) {
		t.Errorf("after starting-amount fix: paid %s left %s, want $100.00 and $1,110.00", hist[0][0].Paid, left[1].Balance)
	}
}
