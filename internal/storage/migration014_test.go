package storage

import (
	"context"
	"testing"
)

// 014 keeps each marked row's minimum: existing rows get the debt's current
// minimum, capped at what the row paid (or the payment for a deleted debt).
func TestMigration014BackfillsRowMinimums(t *testing.T) {
	db := openRaw(t)
	migrateThrough(t, db, 13)
	mustExec(t, db, `INSERT INTO debts (id, name, balance_cents, apr, minimum_payment_cents) VALUES
		(1, 'Visa', 450000, 24.99, 12000),
		(2, 'Store', 30000, 18, 3500)`)
	mustExec(t, db, `INSERT INTO plan_months (month, status) VALUES ('2026-11-01', 'paid')`)
	mustExec(t, db, `INSERT INTO plan_month_debts (month, debt_id, debt_name, before_cents, interest_cents, paid_cents, left_cents, role) VALUES
		('2026-11-01', 1, 'Visa', 450000, 9371, 32000, 427371, 'Minimum + extra'),
		('2026-11-01', 2, 'Store', 2000, 30, 2030, 0, 'Paid off'),
		('2026-11-01', 9, 'Gone', 1000, 0, 1000, 0, 'Paid off')`)
	if err := migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	want := map[int]int{1: 12000, 2: 2030, 9: 1000}
	rows, err := db.Query(`SELECT debt_id, minimum_cents FROM plan_month_debts`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, minimum int
		if err := rows.Scan(&id, &minimum); err != nil {
			t.Fatal(err)
		}
		if minimum != want[id] {
			t.Errorf("debt %d minimum = %d, want %d", id, minimum, want[id])
		}
	}
}
