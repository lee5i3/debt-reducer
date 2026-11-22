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

func newDebtService(t *testing.T) (DebtService, repository.SettingsRepository) {
	t.Helper()
	db, err := storage.OpenPath(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	settings := repository.NewSettingsRepository(db)
	return NewDebtService(repository.NewDebtRepository(db), settings), settings
}

func validDebt() debt.Debt {
	return debt.Debt{Name: "Visa", Type: debt.TypeCard, Balance: debt.FromDollars(1000), APR: 19.99,
		MinimumPayment: debt.FromDollars(25), DueDay: 15,
		StartDate: time.Date(2026, time.March, 4, 0, 0, 0, 0, time.Local)}
}

func TestDebtServiceCreateValidation(t *testing.T) {
	tests := []struct {
		name   string
		modify func(d *debt.Debt)
	}{
		{"empty name", func(d *debt.Debt) { d.Name = "" }},
		{"blank name", func(d *debt.Debt) { d.Name = "   " }},
		{"zero balance", func(d *debt.Debt) { d.Balance = 0 }},
		{"negative balance", func(d *debt.Debt) { d.Balance = -1 }},
		{"negative apr", func(d *debt.Debt) { d.APR = -0.01 }},
		{"zero minimum", func(d *debt.Debt) { d.MinimumPayment = 0 }},
		{"negative minimum", func(d *debt.Debt) { d.MinimumPayment = -100 }},
		{"unknown type", func(d *debt.Debt) { d.Type = "Mortgage" }},
		{"due day 32", func(d *debt.Debt) { d.DueDay = 32 }},
		{"negative due day", func(d *debt.Debt) { d.DueDay = -1 }},
		{"promo with nothing left", func(d *debt.Debt) {
			d.Type = debt.TypePromo
			d.Promos = []debt.PromoItem{{Remaining: 0, Ends: time.Date(2027, 1, 1, 0, 0, 0, 0, time.Local)}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newDebtService(t)
			d := validDebt()
			tt.modify(&d)
			if _, err := svc.Create(context.Background(), d); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Create: want ErrInvalid, got %v", err)
			}
			if all, _ := svc.List(context.Background()); len(all) != 0 {
				t.Errorf("an invalid debt was stored: %+v", all)
			}

			// Update validates the same way.
			ok, err := svc.Create(context.Background(), validDebt())
			if err != nil {
				t.Fatal(err)
			}
			bad := validDebt()
			tt.modify(&bad)
			bad.ID = ok.ID
			if err := svc.Update(context.Background(), bad); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Update: want ErrInvalid, got %v", err)
			}
			if got, _ := svc.Get(context.Background(), ok.ID); got.Name != "Visa" || got.Balance != debt.FromDollars(1000) {
				t.Errorf("an invalid update was stored: %+v", got)
			}
		})
	}
}

func TestDebtServiceCreateNormalizes(t *testing.T) {
	svc, _ := newDebtService(t)
	ctx := context.Background()

	d := validDebt()
	d.Name = "  Visa  "
	d.Type = ""
	d.StartDate = time.Time{}
	d.Promos = []debt.PromoItem{{Remaining: 500}} // not a promo account: dropped
	created, err := svc.Create(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 {
		t.Fatal("created debt has no ID")
	}
	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Visa" {
		t.Errorf("name = %q, want trimmed", got.Name)
	}
	if got.Type != debt.DefaultType {
		t.Errorf("type = %q, want default %q", got.Type, debt.DefaultType)
	}
	if len(got.Promos) != 0 {
		t.Errorf("non-promo debt kept purchases: %+v", got.Promos)
	}
	if !sameDay(got.StartDate, today()) {
		t.Errorf("start date = %s, want today %s", got.StartDate, today())
	}
	if got.Balance != debt.FromDollars(1000) || got.APR != 19.99 {
		t.Errorf("balance/apr changed for a card: %s %v", got.Balance, got.APR)
	}
}

func TestDebtServicePromoAccount(t *testing.T) {
	svc, _ := newDebtService(t)
	ctx := context.Background()
	jan := time.Date(2027, time.January, 15, 0, 0, 0, 0, time.Local)
	jun := time.Date(2027, time.June, 1, 0, 0, 0, 0, time.Local)
	mar := time.Date(2027, time.March, 9, 0, 0, 0, 0, time.Local)

	d := validDebt()
	d.Type = debt.TypePromo
	d.Balance = debt.FromDollars(1) // replaced by the purchases
	d.APR = 26.99                   // interest-free while the promotions run
	d.Promos = []debt.PromoItem{
		{Remaining: debt.FromDollars(400), Ends: jan},
		{Remaining: debt.FromDollars(250.50), Ends: jun},
		{Remaining: debt.FromDollars(100), Ends: mar},
	}
	created, err := svc.Create(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if created.Balance != debt.FromDollars(750.50) || created.APR != 0 {
		t.Errorf("returned balance %s apr %v, want $750.50 and 0", created.Balance, created.APR)
	}
	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Balance != debt.FromDollars(750.50) {
		t.Errorf("balance = %s, want the sum of purchases $750.50", got.Balance)
	}
	if got.APR != 0 {
		t.Errorf("apr = %v, want 0", got.APR)
	}
	if got.TermEnds == nil || !sameDay(*got.TermEnds, jun) {
		t.Errorf("term ends = %v, want the last expiration %s", got.TermEnds, jun)
	}
	if len(got.Promos) != 3 {
		t.Errorf("purchases = %+v, want 3", got.Promos)
	}

	// Update recomputes from the new purchases.
	got.Promos = []debt.PromoItem{{Remaining: debt.FromDollars(80), Ends: mar}}
	got.Balance = debt.FromDollars(9999)
	if err := svc.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	upd, _ := svc.Get(ctx, got.ID)
	if upd.Balance != debt.FromDollars(80) || upd.TermEnds == nil || !sameDay(*upd.TermEnds, mar) {
		t.Errorf("after update: balance %s term ends %v, want $80 and %s", upd.Balance, upd.TermEnds, mar)
	}

	// A promo account without purchases keeps its own balance, APR and term.
	plain := validDebt()
	plain.Type = debt.TypePromo
	plain.APR = 5
	plain.TermEnds = &jan
	p, err := svc.Create(ctx, plain)
	if err != nil {
		t.Fatal(err)
	}
	if p.Balance != debt.FromDollars(1000) || p.APR != 5 || p.TermEnds == nil || !p.TermEnds.Equal(jan) {
		t.Errorf("promo without purchases = %+v", p)
	}
}

func TestDebtServiceUpdateDeleteNotFound(t *testing.T) {
	svc, _ := newDebtService(t)
	ctx := context.Background()
	d := validDebt()
	d.ID = 42
	if err := svc.Update(ctx, d); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update unknown: want ErrNotFound, got %v", err)
	}
	if err := svc.Delete(ctx, 42); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete unknown: want ErrNotFound, got %v", err)
	}
	if _, err := svc.Get(ctx, 42); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get unknown: want ErrNotFound, got %v", err)
	}

	created, err := svc.Create(ctx, validDebt())
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if all, _ := svc.List(ctx); len(all) != 0 {
		t.Errorf("debt still listed after delete: %+v", all)
	}
}

func TestDebtServiceSetActive(t *testing.T) {
	svc, _ := newDebtService(t)
	ctx := context.Background()
	if err := svc.SetActive(ctx, 99, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetActive unknown id: want ErrNotFound, got %v", err)
	}
	created, err := svc.Create(ctx, validDebt())
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetActive(ctx, created.ID, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Get(ctx, created.ID); !got.Inactive {
		t.Error("SetActive(false) should mark the debt inactive")
	}
	if err := svc.SetActive(ctx, created.ID, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Get(ctx, created.ID); got.Inactive {
		t.Error("SetActive(true) should put the debt back in the plan")
	}
}

func TestDebtServicePreferences(t *testing.T) {
	svc, _ := newDebtService(t)
	ctx := context.Background()

	p, err := svc.Preferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p != debt.DefaultPreferences {
		t.Errorf("fresh preferences = %+v, want %+v", p, debt.DefaultPreferences)
	}

	bad := []debt.Preferences{
		{Theme: "blue", NoticeSeconds: 5},
		{Theme: "", NoticeSeconds: 5},
		{Theme: debt.ThemeDark, NoticeSeconds: -1},
		{Theme: debt.ThemeLight, NoticeSeconds: 61},
	}
	for _, b := range bad {
		if err := svc.SavePreferences(ctx, b); !errors.Is(err, ErrInvalid) {
			t.Errorf("SavePreferences(%+v): want ErrInvalid, got %v", b, err)
		}
	}
	if p, _ := svc.Preferences(ctx); p != debt.DefaultPreferences {
		t.Errorf("invalid preferences were stored: %+v", p)
	}

	for _, good := range []debt.Preferences{
		{Theme: debt.ThemeLight, ExpandCurrent: false, NoticeSeconds: 0},
		{Theme: debt.ThemeDark, ExpandCurrent: true, NoticeSeconds: 60},
	} {
		if err := svc.SavePreferences(ctx, good); err != nil {
			t.Fatalf("SavePreferences(%+v): %v", good, err)
		}
		if got, _ := svc.Preferences(ctx); got != good {
			t.Errorf("preferences = %+v, want %+v", got, good)
		}
	}
}

func TestDebtServiceSaveSettings(t *testing.T) {
	svc, _ := newDebtService(t)
	ctx := context.Background()

	s, err := svc.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.Strategy != debt.Avalanche || s.MonthlyBudget != 0 || s.PlanStart != nil {
		t.Errorf("fresh settings = %+v", s)
	}

	for _, bad := range []debt.Settings{
		{Strategy: "fastest"},
		{Strategy: ""},
		{Strategy: debt.Snowball, MonthlyBudget: -1},
	} {
		if err := svc.SaveSettings(ctx, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("SaveSettings(%+v): want ErrInvalid, got %v", bad, err)
		}
	}

	want := debt.Settings{Strategy: debt.Custom, MonthlyBudget: debt.FromDollars(750), Extra: debt.FromDollars(25),
		Order: debt.Order{By: debt.OrderByBalance, First: debt.OrderLowFirst}}
	if err := svc.SaveSettings(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Settings(ctx)
	if got.Strategy != want.Strategy || got.MonthlyBudget != want.MonthlyBudget || got.Extra != want.Extra || got.Order != want.Order {
		t.Errorf("settings = %+v, want %+v", got, want)
	}
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
}
