package repository

import (
	"context"
	"errors"
	"testing"

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

	created, err := repo.Create(ctx, debt.Debt{Name: "Visa", Balance: 450000, APR: 24.99, MinimumPayment: 12000, DueDay: 15})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 {
		t.Fatal("expected id to be set")
	}

	created.Balance = 400000
	if err := repo.Update(ctx, created); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != created {
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
	want := debt.Settings{MonthlyBudget: 150000, Strategy: debt.Snowball}
	if err := repo.Save(ctx, want); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.Get(ctx); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
