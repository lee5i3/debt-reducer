package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/lee5i3/debt-reducer/internal/debt"
	"github.com/lee5i3/debt-reducer/internal/storage"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.OpenPath(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.Local) }

func money(v debt.Money) *debt.Money { return &v }

func TestDebtInactiveAndStartDateRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo := NewDebtRepository(openTestDB(t))

	start := day(2026, time.March, 9)
	d, err := repo.Create(ctx, debt.Debt{Name: "Car", Type: debt.TypeLoan, Balance: 800000, APR: 6.5, MinimumPayment: 25000, StartDate: start, Inactive: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Inactive {
		t.Error("Inactive should round-trip true")
	}
	if !got.StartDate.Equal(start) {
		t.Errorf("StartDate = %s, want %s", got.StartDate, start)
	}
	if got.DueDay != 0 || got.TermEnds != nil || got.Promos != nil {
		t.Errorf("unset fields came back as %+v", got)
	}

	got.Inactive = false
	got.StartDate = day(2026, time.April, 1)
	if err := repo.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	again, _ := repo.Get(ctx, d.ID)
	if again.Inactive || !again.StartDate.Equal(day(2026, time.April, 1)) {
		t.Errorf("after update: inactive %v, start %s", again.Inactive, again.StartDate)
	}

	// A paid-off debt keeps a zero balance.
	again.Balance = 0
	if err := repo.Update(ctx, again); err != nil {
		t.Fatalf("zero balance should be allowed: %v", err)
	}
	if z, _ := repo.Get(ctx, d.ID); z.Balance != 0 {
		t.Errorf("balance = %s, want 0", z.Balance)
	}
}

func TestDebtUpdateUnknownID(t *testing.T) {
	repo := NewDebtRepository(openTestDB(t))
	err := repo.Update(context.Background(), debt.Debt{ID: 999, Name: "x", Balance: 1, MinimumPayment: 1})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestDebtCreateRejectsBadRow(t *testing.T) {
	repo := NewDebtRepository(openTestDB(t))
	// The schema requires a positive minimum payment.
	if _, err := repo.Create(context.Background(), debt.Debt{Name: "x", Balance: 100, MinimumPayment: 0}); err == nil {
		t.Error("zero minimum should fail the CHECK constraint")
	}
	if list, _ := repo.List(context.Background()); len(list) != 0 {
		t.Errorf("failed create left %d rows", len(list))
	}
}

func TestSetInactive(t *testing.T) {
	ctx := context.Background()
	repo := NewDebtRepository(openTestDB(t))
	d, err := repo.Create(ctx, debt.Debt{Name: "Visa", Type: debt.TypeCard, Balance: 1000, MinimumPayment: 100, StartDate: day(2026, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetInactive(ctx, d.ID, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.Get(ctx, d.ID); !got.Inactive {
		t.Error("should be inactive")
	}
	if err := repo.SetInactive(ctx, d.ID, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.Get(ctx, d.ID); got.Inactive {
		t.Error("should be active again")
	}
	if err := repo.SetInactive(ctx, 12345, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id err = %v, want ErrNotFound", err)
	}
}

func TestListOrderAndPromos(t *testing.T) {
	ctx := context.Background()
	repo := NewDebtRepository(openTestDB(t))
	if list, err := repo.List(ctx); err != nil || list == nil || len(list) != 0 {
		t.Fatalf("empty list = %#v, %v (want empty non-nil slice)", list, err)
	}
	mk := func(name string, promos ...debt.PromoItem) debt.Debt {
		typ := debt.TypeCard
		if len(promos) > 0 {
			typ = debt.TypePromo
		}
		d, err := repo.Create(ctx, debt.Debt{Name: name, Type: typ, Balance: 5000, MinimumPayment: 100, StartDate: day(2026, 1, 1), Promos: promos})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	mk("visa")
	store := mk("Store", debt.PromoItem{Remaining: 3000, Ends: day(2028, 5, 1)}, debt.PromoItem{Remaining: 2000, Ends: day(2027, 5, 1)})
	mk("Amex")

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, d := range list {
		names = append(names, d.Name)
	}
	if len(names) != 3 || names[0] != "Amex" || names[1] != "Store" || names[2] != "visa" {
		t.Errorf("order = %v, want case-insensitive by name", names)
	}
	for _, d := range list {
		if d.ID == store.ID {
			if len(d.Promos) != 2 || !d.Promos[0].Ends.Equal(day(2027, 5, 1)) || d.Promos[0].Remaining != 2000 || d.Promos[0].ID == 0 {
				t.Errorf("store promos = %+v", d.Promos)
			}
		} else if len(d.Promos) != 0 {
			t.Errorf("%s promos = %+v, want none", d.Name, d.Promos)
		}
	}
}

func TestPromoItemsReplacedOnUpdateAndDeleted(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewDebtRepository(db)
	d, err := repo.Create(ctx, debt.Debt{Name: "Store", Type: debt.TypePromo, Balance: 5000, MinimumPayment: 100, StartDate: day(2026, 1, 1),
		Promos: []debt.PromoItem{{Remaining: 3000, Ends: day(2028, 1, 1)}, {Remaining: 2000, Ends: day(2027, 1, 1)}}})
	if err != nil {
		t.Fatal(err)
	}

	d.Promos = []debt.PromoItem{{Remaining: 4500, Ends: day(2029, 6, 30)}}
	if err := repo.Update(ctx, d); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.Get(ctx, d.ID)
	if len(got.Promos) != 1 || got.Promos[0].Remaining != 4500 || !got.Promos[0].Ends.Equal(day(2029, 6, 30)) {
		t.Fatalf("promos after update = %+v", got.Promos)
	}

	// Clearing them.
	d.Promos = nil
	if err := repo.Update(ctx, d); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.Get(ctx, d.ID); len(got.Promos) != 0 {
		t.Errorf("promos after clearing = %+v", got.Promos)
	}

	// Deleting a debt removes its purchases (foreign key cascade).
	d.Promos = []debt.PromoItem{{Remaining: 1, Ends: day(2029, 1, 1)}}
	if err := repo.Update(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM promo_items`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d promo items left after delete", n)
	}
}

func TestSettingsExtraAndNormalize(t *testing.T) {
	ctx := context.Background()
	repo := NewSettingsRepository(openTestDB(t))

	s, err := repo.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.Extra != 0 || s.PlanStart != nil || s.StartTotal != 0 || s.Order != (debt.Order{By: debt.OrderByInterest, First: debt.OrderHighFirst}) {
		t.Errorf("defaults = %+v", s)
	}

	// Unknown order values are normalized on save.
	if err := repo.Save(ctx, debt.Settings{Extra: 25000, Strategy: debt.Snowball, Order: debt.Order{By: "junk"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.Get(ctx)
	if got.Extra != 25000 || got.Strategy != debt.Snowball || got.MonthlyBudget != 0 {
		t.Errorf("saved = %+v", got)
	}
	if got.Order != (debt.Order{By: debt.OrderByInterest, First: debt.OrderHighFirst}) {
		t.Errorf("order = %+v, want normalized", got.Order)
	}

	// Save doesn't touch the plan start.
	if err := repo.SavePlanStart(ctx, day(2026, time.December, 1), 1234567); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, debt.Settings{Extra: 100, Strategy: debt.Avalanche}); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.Get(ctx)
	if got.PlanStart == nil || !got.PlanStart.Equal(day(2026, time.December, 1)) || got.StartTotal != 1234567 {
		t.Errorf("plan start = %v, total %d", got.PlanStart, got.StartTotal)
	}
	if got.Extra != 100 {
		t.Errorf("extra = %d", got.Extra)
	}

	// Negative extra is rejected by the schema.
	if err := repo.Save(ctx, debt.Settings{Extra: -1, Strategy: debt.Avalanche}); err == nil {
		t.Error("negative extra should fail")
	}
}

func TestSavePlanStartOverwrites(t *testing.T) {
	ctx := context.Background()
	repo := NewSettingsRepository(openTestDB(t))
	if err := repo.SavePlanStart(ctx, day(2026, 11, 1), 500); err != nil {
		t.Fatal(err)
	}
	if err := repo.SavePlanStart(ctx, day(2027, 2, 1), 900); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.Get(ctx)
	if got.PlanStart == nil || !got.PlanStart.Equal(day(2027, 2, 1)) || got.StartTotal != 900 {
		t.Errorf("got %v / %d", got.PlanStart, got.StartTotal)
	}
}

func TestPreferences(t *testing.T) {
	ctx := context.Background()
	repo := NewSettingsRepository(openTestDB(t))

	p, err := repo.Preferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p != debt.DefaultPreferences {
		t.Errorf("new database preferences = %+v, want %+v", p, debt.DefaultPreferences)
	}

	want := debt.Preferences{Theme: debt.ThemeLight, ExpandCurrent: false, NoticeSeconds: 0}
	if err := repo.SavePreferences(ctx, want); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.Preferences(ctx); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	want = debt.Preferences{Theme: debt.ThemeDark, ExpandCurrent: true, NoticeSeconds: 30}
	if err := repo.SavePreferences(ctx, want); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.Preferences(ctx); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}

	// The schema rejects bad values and keeps the old ones.
	if err := repo.SavePreferences(ctx, debt.Preferences{Theme: "blue", NoticeSeconds: 5}); err == nil {
		t.Error("unknown theme should fail")
	}
	if err := repo.SavePreferences(ctx, debt.Preferences{Theme: debt.ThemeDark, NoticeSeconds: 61}); err == nil {
		t.Error("notice seconds over 60 should fail")
	}
	if got, _ := repo.Preferences(ctx); got != want {
		t.Errorf("after rejected saves got %+v, want %+v", got, want)
	}

	// Preferences don't disturb the plan settings.
	if err := repo.Save(ctx, debt.Settings{Extra: 700, Strategy: debt.Snowball}); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.Preferences(ctx); got != want {
		t.Errorf("Save changed preferences: %+v", got)
	}
}

func TestPlanMonthsEmpty(t *testing.T) {
	repo := NewPlanRepository(openTestDB(t))
	months, err := repo.Months(context.Background())
	if err != nil || len(months) != 0 {
		t.Errorf("months = %+v, %v", months, err)
	}
}

func TestPlanRecordAndMonthsOrder(t *testing.T) {
	ctx := context.Background()
	repo := NewPlanRepository(openTestDB(t))
	dec, jan := day(2026, time.December, 1), day(2027, time.January, 1)

	// Recorded out of order; Months returns oldest first.
	janRec := debt.MonthRecord{Month: jan, Status: debt.MonthSkipped, Interest: 900, Debts: []debt.MonthDebt{
		{DebtID: 2, Name: "Visa", Before: 100000, Interest: 900, Left: 100900, Role: debt.RoleSkipped},
	}}
	decRec := debt.MonthRecord{Month: dec, Status: debt.MonthPaid, Paid: 50000, Interest: 1500, Debts: []debt.MonthDebt{
		{DebtID: 2, Name: "Visa", Before: 120000, Interest: 1000, Paid: 21000, Left: 100000, Role: debt.RoleExtra},
		{DebtID: 1, Name: "Car", Before: 300000, Interest: 500, Paid: 29000, Left: 271500, Role: debt.RoleMinimum},
	}}
	if err := repo.Record(ctx, janRec); err != nil {
		t.Fatal(err)
	}
	if err := repo.Record(ctx, decRec); err != nil {
		t.Fatal(err)
	}
	months, err := repo.Months(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(months) != 2 || !months[0].Month.Equal(dec) || !months[1].Month.Equal(jan) {
		t.Fatalf("months = %+v", months)
	}
	m := months[0]
	if m.Status != debt.MonthPaid || m.Paid != 50000 || m.Interest != 1500 || m.Override != nil || m.Extra != nil || !m.Marked() {
		t.Errorf("dec = %+v", m)
	}
	// Debts keep their insert order and all fields.
	if len(m.Debts) != 2 || m.Debts[0].Name != "Visa" || m.Debts[1].Name != "Car" {
		t.Fatalf("dec debts = %+v", m.Debts)
	}
	v := m.Debts[0]
	if v.DebtID != 2 || v.Before != 120000 || v.Interest != 1000 || v.Paid != 21000 || v.Left != 100000 || v.Role != debt.RoleExtra {
		t.Errorf("visa row = %+v", v)
	}
	if months[1].Status != debt.MonthSkipped || len(months[1].Debts) != 1 || months[1].Debts[0].Role != debt.RoleSkipped {
		t.Errorf("jan = %+v", months[1])
	}

	// Recording the same month again replaces its debts.
	decRec.Debts = decRec.Debts[:1]
	decRec.Paid = 21000
	if err := repo.Record(ctx, decRec); err != nil {
		t.Fatal(err)
	}
	months, _ = repo.Months(ctx)
	if months[0].Paid != 21000 || len(months[0].Debts) != 1 {
		t.Errorf("re-record = %+v", months[0])
	}
}

func TestPlanRecordRejectsBadStatus(t *testing.T) {
	repo := NewPlanRepository(openTestDB(t))
	err := repo.Record(context.Background(), debt.MonthRecord{Month: day(2026, 12, 1), Status: "late",
		Debts: []debt.MonthDebt{{DebtID: 1, Name: "x", Role: debt.RoleMinimum}}})
	if err == nil {
		t.Fatal("unknown status should fail")
	}
	if months, _ := repo.Months(context.Background()); len(months) != 0 {
		t.Errorf("failed record left %+v", months)
	}
}

func TestPlanRecordRollsBackOnDebtError(t *testing.T) {
	ctx := context.Background()
	repo := NewPlanRepository(openTestDB(t))
	// Two rows for the same debt break the (month, debt_id) primary key;
	// the whole record must roll back.
	err := repo.Record(ctx, debt.MonthRecord{Month: day(2026, 12, 1), Status: debt.MonthPaid, Debts: []debt.MonthDebt{
		{DebtID: 1, Name: "a", Role: debt.RoleMinimum},
		{DebtID: 1, Name: "a", Role: debt.RoleMinimum},
	}})
	if err == nil {
		t.Fatal("duplicate debt rows should fail")
	}
	if months, _ := repo.Months(ctx); len(months) != 0 {
		t.Errorf("record not rolled back: %+v", months)
	}
}

func TestPlanUndoKeepsEdits(t *testing.T) {
	ctx := context.Background()
	repo := NewPlanRepository(openTestDB(t))
	dec := day(2026, time.December, 1)

	if err := repo.SetOverride(ctx, dec, money(70000)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Record(ctx, debt.MonthRecord{Month: dec, Status: debt.MonthPaid, Paid: 70000, Interest: 100,
		Debts: []debt.MonthDebt{{DebtID: 1, Name: "Visa", Paid: 70000, Role: debt.RoleExtra}}}); err != nil {
		t.Fatal(err)
	}
	months, _ := repo.Months(ctx)
	if len(months) != 1 || months[0].Override == nil || *months[0].Override != 70000 || months[0].Status != debt.MonthPaid {
		t.Fatalf("record should keep the override: %+v", months)
	}

	if err := repo.Undo(ctx, dec); err != nil {
		t.Fatal(err)
	}
	months, _ = repo.Months(ctx)
	if len(months) != 1 {
		t.Fatalf("undo should keep an edited month: %+v", months)
	}
	m := months[0]
	if m.Status != "" || m.Paid != 0 || m.Interest != 0 || len(m.Debts) != 0 || m.Marked() {
		t.Errorf("after undo = %+v", m)
	}
	if m.Override == nil || *m.Override != 70000 {
		t.Errorf("override lost: %v", m.Override)
	}

	// Same with a one-time payment.
	jan := day(2027, time.January, 1)
	if err := repo.SetExtra(ctx, jan, money(5000)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Record(ctx, debt.MonthRecord{Month: jan, Status: debt.MonthSkipped}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Undo(ctx, jan); err != nil {
		t.Fatal(err)
	}
	months, _ = repo.Months(ctx)
	if len(months) != 2 || months[1].Extra == nil || *months[1].Extra != 5000 || months[1].Status != "" {
		t.Errorf("after undo with extra = %+v", months)
	}

	// Undo of a month that was never stored is a no-op.
	if err := repo.Undo(ctx, day(2030, 1, 1)); err != nil {
		t.Errorf("undo unknown month: %v", err)
	}
}

func TestPlanSetOverrideAndDropEmpty(t *testing.T) {
	ctx := context.Background()
	repo := NewPlanRepository(openTestDB(t))
	feb := day(2027, time.February, 1)

	if err := repo.SetOverride(ctx, feb, money(40000)); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetOverride(ctx, feb, money(45000)); err != nil {
		t.Fatal(err)
	}
	months, _ := repo.Months(ctx)
	if len(months) != 1 || *months[0].Override != 45000 || months[0].Marked() {
		t.Fatalf("override = %+v", months)
	}
	// Zero is a valid override (pay nothing).
	if err := repo.SetOverride(ctx, feb, money(0)); err != nil {
		t.Fatal(err)
	}
	if months, _ = repo.Months(ctx); months[0].Override == nil || *months[0].Override != 0 {
		t.Errorf("zero override = %+v", months[0].Override)
	}
	if err := repo.SetOverride(ctx, feb, money(-1)); err == nil {
		t.Error("negative override should fail")
	}

	// With an extra still set, clearing the override keeps the row.
	if err := repo.SetExtra(ctx, feb, money(1000)); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetOverride(ctx, feb, nil); err != nil {
		t.Fatal(err)
	}
	months, _ = repo.Months(ctx)
	if len(months) != 1 || months[0].Override != nil || months[0].Extra == nil || *months[0].Extra != 1000 {
		t.Fatalf("after clearing override = %+v", months)
	}
	// Clearing the extra too drops the now-empty row.
	if err := repo.SetExtra(ctx, feb, nil); err != nil {
		t.Fatal(err)
	}
	if months, _ = repo.Months(ctx); len(months) != 0 {
		t.Errorf("empty month should be dropped: %+v", months)
	}

	// Clearing something on a month that doesn't exist is fine.
	if err := repo.SetOverride(ctx, day(2031, 1, 1), nil); err != nil {
		t.Error(err)
	}
	if err := repo.SetExtra(ctx, day(2031, 1, 1), nil); err != nil {
		t.Error(err)
	}
}

func TestPlanDropEmptyKeepsMarkedMonth(t *testing.T) {
	ctx := context.Background()
	repo := NewPlanRepository(openTestDB(t))
	mar := day(2027, time.March, 1)
	if err := repo.SetOverride(ctx, mar, money(100)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Record(ctx, debt.MonthRecord{Month: mar, Status: debt.MonthPaid, Paid: 100}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetOverride(ctx, mar, nil); err != nil {
		t.Fatal(err)
	}
	months, _ := repo.Months(ctx)
	if len(months) != 1 || months[0].Status != debt.MonthPaid || months[0].Override != nil || months[0].Paid != 100 {
		t.Errorf("marked month must stay: %+v", months)
	}
}

func TestPlanSetExtraUpserts(t *testing.T) {
	ctx := context.Background()
	repo := NewPlanRepository(openTestDB(t))
	apr := day(2027, time.April, 1)
	if err := repo.SetExtra(ctx, apr, money(2500)); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetOverride(ctx, apr, money(9000)); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetExtra(ctx, apr, money(3000)); err != nil {
		t.Fatal(err)
	}
	months, _ := repo.Months(ctx)
	if len(months) != 1 || *months[0].Extra != 3000 || *months[0].Override != 9000 {
		t.Errorf("months = %+v", months)
	}
	if err := repo.SetExtra(ctx, apr, money(-5)); err == nil {
		t.Error("negative extra should fail")
	}
}

func TestPlanBudgetChanges(t *testing.T) {
	ctx := context.Background()
	repo := NewPlanRepository(openTestDB(t))

	if got, err := repo.BudgetChanges(ctx); err != nil || len(got) != 0 {
		t.Fatalf("empty = %+v, %v", got, err)
	}
	in := []debt.BudgetChange{
		{Month: day(2027, time.June, 1), Amount: 90000},
		{Month: day(2027, time.January, 1), Amount: 80000},
	}
	if err := repo.SetBudgetChanges(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := repo.BudgetChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].Month.Equal(day(2027, 1, 1)) || got[0].Amount != 80000 || !got[1].Month.Equal(day(2027, 6, 1)) || got[1].Amount != 90000 {
		t.Errorf("changes = %+v, want earliest first", got)
	}

	// Replaced wholesale.
	if err := repo.SetBudgetChanges(ctx, []debt.BudgetChange{{Month: day(2028, 3, 1), Amount: 0}}); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.BudgetChanges(ctx)
	if len(got) != 1 || !got[0].Month.Equal(day(2028, 3, 1)) || got[0].Amount != 0 {
		t.Errorf("after replace = %+v", got)
	}

	// A failing set (duplicate month) rolls back and keeps the old list.
	err = repo.SetBudgetChanges(ctx, []debt.BudgetChange{{Month: day(2029, 1, 1), Amount: 1}, {Month: day(2029, 1, 1), Amount: 2}})
	if err == nil {
		t.Fatal("duplicate months should fail")
	}
	got, _ = repo.BudgetChanges(ctx)
	if len(got) != 1 || !got[0].Month.Equal(day(2028, 3, 1)) {
		t.Errorf("not rolled back: %+v", got)
	}

	// Empty clears everything.
	if err := repo.SetBudgetChanges(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ = repo.BudgetChanges(ctx); len(got) != 0 {
		t.Errorf("after clear = %+v", got)
	}
}

func TestPlanAllocations(t *testing.T) {
	ctx := context.Background()
	repo := NewPlanRepository(openTestDB(t))
	may, jun := day(2027, time.May, 1), day(2027, time.June, 1)

	got, err := repo.Allocations(ctx)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty = %#v, %v", got, err)
	}
	if err := repo.SetAllocations(ctx, may, map[int64]debt.Money{1: 25000, 2: 12000}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetAllocations(ctx, jun, map[int64]debt.Money{2: 0}); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.Allocations(ctx)
	if len(got) != 2 || got[may][1] != 25000 || got[may][2] != 12000 || len(got[jun]) != 1 {
		t.Fatalf("allocations = %+v", got)
	}
	if v, ok := got[jun][2]; !ok || v != 0 {
		t.Errorf("zero allocation should be kept: %+v", got[jun])
	}

	// Replacing one month leaves the other alone.
	if err := repo.SetAllocations(ctx, may, map[int64]debt.Money{3: 500}); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.Allocations(ctx)
	if len(got[may]) != 1 || got[may][3] != 500 || len(got[jun]) != 1 {
		t.Errorf("after replace = %+v", got)
	}

	// nil clears a month.
	if err := repo.SetAllocations(ctx, may, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.Allocations(ctx)
	if _, ok := got[may]; ok || len(got) != 1 {
		t.Errorf("after clear = %+v", got)
	}

	// Negative amounts are rejected and roll back the whole month.
	if err := repo.SetAllocations(ctx, jun, map[int64]debt.Money{5: -1}); err == nil {
		t.Error("negative allocation should fail")
	}
	got, _ = repo.Allocations(ctx)
	if len(got[jun]) != 1 {
		t.Errorf("failed set should roll back: %+v", got)
	}

	// Allocations are kept apart from plan_months: setting them doesn't
	// create a month row.
	if months, _ := repo.Months(ctx); len(months) != 0 {
		t.Errorf("allocations created month rows: %+v", months)
	}
}
