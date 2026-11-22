package repository

import (
	"context"
	"testing"
	"time"

	"github.com/lee5i3/debt-reducer/internal/debt"
	"github.com/lee5i3/debt-reducer/internal/storage"
)

func TestRecordKeepsEachRowsMinimum(t *testing.T) {
	db, err := storage.OpenPath(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewPlanRepository(db)
	ctx := context.Background()
	nov := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.Local)
	rec := debt.MonthRecord{Month: nov, Status: debt.MonthPaid, Paid: debt.FromDollars(130), Debts: []debt.MonthDebt{
		{DebtID: 1, Name: "Visa", Before: debt.FromDollars(1000), Paid: debt.FromDollars(100), Minimum: debt.FromDollars(40), Left: debt.FromDollars(900), Role: debt.RoleExtra},
		// A minimum above what was paid is stored capped at the payment.
		{DebtID: 2, Name: "Store", Before: debt.FromDollars(30), Paid: debt.FromDollars(30), Minimum: debt.FromDollars(35), Role: debt.RolePaidOff},
	}}
	if err := repo.Record(ctx, rec); err != nil {
		t.Fatal(err)
	}
	months, err := repo.Months(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := months[0].Debts
	if got[0].Minimum != debt.FromDollars(40) || got[0].Extra() != debt.FromDollars(60) {
		t.Errorf("Visa row = %+v, want minimum $40 and extra $60", got[0])
	}
	if got[1].Minimum != debt.FromDollars(30) {
		t.Errorf("Store minimum = %s, want capped at $30.00", got[1].Minimum)
	}
}
