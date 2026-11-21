package repository

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/lee5i3/debt-reducer/internal/debt"
	"github.com/lee5i3/debt-reducer/internal/storage"
)

func TestDebtCRUD(t *testing.T) {
	db, err := storage.OpenPath(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	repo := NewDebtRepository(db)

	created, err := repo.Create(ctx, debt.Debt{Name: "Visa", Type: "Credit Card", StartDate: time.Date(2026, time.November, 21, 0, 0, 0, 0, time.Local), Balance: 450000, APR: 24.99, MinimumPayment: 12000, DueDay: 15})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 {
		t.Fatal("expected id to be set")
	}

	ends := time.Date(2028, time.March, 1, 0, 0, 0, 0, time.Local)
	created.Balance = 400000
	created.TermEnds = &ends
	if err := repo.Update(ctx, created); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TermEnds == nil || !got.TermEnds.Equal(ends) {
		t.Fatalf("term ends = %v, want %v", got.TermEnds, ends)
	}
	got.TermEnds, created.TermEnds = nil, nil
	if !reflect.DeepEqual(got, created) {
		t.Fatalf("got %+v, want %+v", got, created)
	}

	list, err := repo.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v, %v", list, err)
	}

	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := repo.Delete(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound on second delete, got %v", err)
	}
}

func TestSettings(t *testing.T) {
	db, err := storage.OpenPath(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	repo := NewSettingsRepository(db)

	s, err := repo.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.Strategy != debt.Avalanche || s.MonthlyBudget != 0 {
		t.Fatalf("unexpected defaults %+v", s)
	}
	want := debt.Settings{MonthlyBudget: 150000, Strategy: debt.Custom, Order: debt.Order{By: debt.OrderByBalance, First: debt.OrderLowFirst}}
	if err := repo.Save(ctx, want); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.Get(ctx); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestRecordLeavesDebtsAlone(t *testing.T) {
	db, err := storage.OpenPath(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	debts := NewDebtRepository(db)
	plans := NewPlanRepository(db)

	apr := time.Date(2029, time.April, 13, 0, 0, 0, 0, time.Local)
	jun := time.Date(2029, time.June, 13, 0, 0, 0, 0, time.Local)
	d, err := debts.Create(ctx, debt.Debt{
		Name: "Store", Type: debt.TypePromo, Balance: debt.FromDollars(1836.50), MinimumPayment: debt.FromDollars(62.38), DueDay: 6,
		StartDate: time.Date(2026, time.November, 21, 0, 0, 0, 0, time.Local),
		Promos: []debt.PromoItem{
			{Remaining: debt.FromDollars(427.12), Ends: jun},
			{Remaining: debt.FromDollars(1409.38), Ends: apr},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := debts.Get(ctx, d.ID)
	if len(got.Promos) != 2 || !got.Promos[0].Ends.Equal(apr) {
		t.Fatalf("promos should load earliest-expiring first: %+v", got.Promos)
	}

	month := time.Date(2026, time.December, 1, 0, 0, 0, 0, time.Local)
	rec := debt.MonthRecord{Month: month, Status: debt.MonthPaid, Paid: debt.FromDollars(100), Debts: []debt.MonthDebt{
		{DebtID: d.ID, Name: "Store", Before: debt.FromDollars(1836.50), Paid: debt.FromDollars(100), Left: debt.FromDollars(1736.50), Role: debt.RoleExtra},
	}}
	if err := plans.Record(ctx, rec); err != nil {
		t.Fatal(err)
	}
	got, _ = debts.Get(ctx, d.ID)
	if got.Balance != debt.FromDollars(1836.50) || got.Promos[0].Remaining != debt.FromDollars(1409.38) {
		t.Fatalf("recording a month must not change the debt: balance %s, promos %+v", got.Balance, got.Promos)
	}
	months, _ := plans.Months(ctx)
	if len(months) != 1 || len(months[0].Debts) != 1 || months[0].Debts[0].Paid != debt.FromDollars(100) {
		t.Fatalf("month not recorded: %+v", months)
	}
	if err := plans.Undo(ctx, month); err != nil {
		t.Fatal(err)
	}
	if months, _ = plans.Months(ctx); len(months) != 0 {
		t.Fatalf("undo should remove the month: %+v", months)
	}
}
