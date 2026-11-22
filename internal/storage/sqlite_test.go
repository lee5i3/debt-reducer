package storage

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/lee5i3/debt-reducer/internal/config"
)

// openRaw opens an empty in-memory database the same way OpenPath does, but
// without migrating it.
func openRaw(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

// migrateThrough applies the embedded migrations up to and including
// version last, recording them in schema_migrations so a later migrate call
// picks up from there.
func migrateThrough(t *testing.T, db *sql.DB, last int) {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	for _, f := range files {
		v, err := strconv.Atoi(strings.SplitN(filepath.Base(f), "_", 2)[0])
		if err != nil {
			t.Fatal(err)
		}
		if v > last {
			break
		}
		body, err := migrations.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatalf("migration %s: %v", f, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, v); err != nil {
			t.Fatal(err)
		}
	}
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func columns(t *testing.T, db *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		out[name] = true
	}
	return out
}

func migrationCount(t *testing.T) int {
	t.Helper()
	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	return len(files)
}

func TestOpenPathRunsAllMigrations(t *testing.T) {
	db, err := OpenPath(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var n, maxV int
	if err := db.QueryRow(`SELECT COUNT(*), MAX(version) FROM schema_migrations`).Scan(&n, &maxV); err != nil {
		t.Fatal(err)
	}
	if want := migrationCount(t); n != want || maxV != want {
		t.Fatalf("schema_migrations has %d rows (max %d), want %d", n, maxV, want)
	}

	want := map[string][]string{
		"debts": {"id", "name", "type", "balance_cents", "apr", "minimum_payment_cents", "due_day",
			"term_ends", "start_date", "inactive", "created_at", "updated_at"},
		"settings": {"id", "monthly_budget_cents", "strategy", "plan_start", "start_total_cents", "order_by",
			"order_first", "theme", "expand_current", "extra_cents", "notice_seconds"},
		"plan_months":            {"month", "status", "override_cents", "paid_cents", "interest_cents", "marked_at", "extra_cents"},
		"plan_month_debts":       {"month", "debt_id", "debt_name", "before_cents", "interest_cents", "paid_cents", "left_cents", "role", "items_before"},
		"promo_items":            {"id", "debt_id", "remaining_cents", "ends"},
		"plan_budget_changes":    {"month", "amount_cents"},
		"plan_month_allocations": {"month", "debt_id", "amount_cents"},
	}
	for table, cols := range want {
		have := columns(t, db, table)
		if len(have) == 0 {
			t.Errorf("table %s is missing", table)
			continue
		}
		for _, c := range cols {
			if !have[c] {
				t.Errorf("%s.%s is missing", table, c)
			}
		}
	}

	// The single settings row exists with its defaults.
	var budget, extra, notice, expand int
	var strategy, theme string
	if err := db.QueryRow(`SELECT monthly_budget_cents, extra_cents, notice_seconds, expand_current, strategy, theme FROM settings WHERE id = 1`).
		Scan(&budget, &extra, &notice, &expand, &strategy, &theme); err != nil {
		t.Fatal(err)
	}
	if budget != 0 || extra != 0 || notice != 5 || expand != 1 || strategy != "avalanche" || theme != "dark" {
		t.Errorf("settings defaults = budget %d extra %d notice %d expand %d strategy %q theme %q",
			budget, extra, notice, expand, strategy, theme)
	}
}

func TestMigrateIsIdempotentOnReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "test.db")
	db, err := OpenPath(path)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `INSERT INTO debts (name, balance_cents, apr, minimum_payment_cents) VALUES ('Visa', 1000, 10, 100)`)
	db.Close()

	// Reopening runs nothing new and keeps the data. Open (the dig
	// constructor) goes through the config path.
	db, err = Open(&config.Config{DBPath: path})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n, debts int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM debts`).Scan(&debts); err != nil {
		t.Fatal(err)
	}
	if n != migrationCount(t) || debts != 1 {
		t.Errorf("after reopen: %d migrations recorded, %d debts; want %d and 1", n, debts, migrationCount(t))
	}
}

func TestMigrateResumesFromRecordedVersion(t *testing.T) {
	db := openRaw(t)
	migrateThrough(t, db, 5)
	if columns(t, db, "promo_items")["id"] {
		t.Fatal("promo_items should not exist before migration 006")
	}
	if err := migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if !columns(t, db, "promo_items")["id"] || !columns(t, db, "settings")["notice_seconds"] {
		t.Error("migrate should apply 006 onward")
	}
}

func TestMigrationFailureRollsBack(t *testing.T) {
	db := openRaw(t)
	migrateThrough(t, db, 8)
	// A column that 009 adds already exists, so 009 fails.
	mustExec(t, db, `ALTER TABLE debts ADD COLUMN start_date TEXT`)
	err := migrate(context.Background(), db)
	if err == nil || !strings.Contains(err.Error(), "009_debt_start_date.sql") {
		t.Fatalf("want an error naming 009, got %v", err)
	}
	var maxV int
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&maxV); err != nil {
		t.Fatal(err)
	}
	if maxV != 8 {
		t.Errorf("schema version = %d after failed migration, want 8", maxV)
	}
	if columns(t, db, "settings")["theme"] {
		t.Error("later migrations should not run after a failure")
	}
}

func TestMigration009RestoresStartingAmounts(t *testing.T) {
	db := openRaw(t)
	migrateThrough(t, db, 8)

	// Debt 1 was marked in September and October, which lowered its balance.
	mustExec(t, db, `INSERT INTO debts (id, name, type, balance_cents, apr, minimum_payment_cents, created_at)
		VALUES (1, 'Visa', 'Credit Card', 46000, 20, 1000, '2026-08-15 10:00:00')`)
	// Debt 2 was never in a marked month.
	mustExec(t, db, `INSERT INTO debts (id, name, type, balance_cents, apr, minimum_payment_cents, created_at)
		VALUES (2, 'Car', 'Loan', 900000, 5, 30000, '2026-07-04 08:30:00')`)
	// Debt 3 is a promotional account with two purchases.
	mustExec(t, db, `INSERT INTO debts (id, name, type, balance_cents, apr, minimum_payment_cents, term_ends, created_at)
		VALUES (3, 'Store', 'Promotional Purchase', 30000, 0, 2500, '2027-06-01', '2026-08-01 00:00:00')`)
	mustExec(t, db, `INSERT INTO promo_items (id, debt_id, remaining_cents, ends) VALUES (10, 3, 10000, '2027-01-01'), (11, 3, 20000, '2027-06-01'), (12, 3, 7000, '2027-09-01')`)

	mustExec(t, db, `INSERT INTO plan_months (month, status) VALUES ('2026-09-01', 'paid'), ('2026-10-01', 'paid')`)
	// Inserted out of order to check the earliest month wins, not the first row.
	mustExec(t, db, `INSERT INTO plan_month_debts (month, debt_id, debt_name, before_cents, interest_cents, paid_cents, left_cents, role, items_before)
		VALUES ('2026-10-01', 1, 'Visa', 48000, 800, 2800, 46000, 'Minimum', NULL),
		       ('2026-09-01', 1, 'Visa', 50000, 830, 2830, 48000, 'Minimum', NULL),
		       ('2026-10-01', 3, 'Store', 35000, 0, 5000, 30000, 'Minimum', '[{"id":10,"remaining":15000},{"id":11,"remaining":20000}]'),
		       ('2026-09-01', 3, 'Store', 40000, 0, 5000, 35000, 'Minimum', '[{"id":10,"remaining":20000},{"id":11,"remaining":20000}]')`,
	)

	if err := migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	type row struct {
		balance int
		start   string
	}
	got := map[int]row{}
	rows, err := db.Query(`SELECT id, balance_cents, start_date FROM debts`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int
		var r row
		if err := rows.Scan(&id, &r.balance, &r.start); err != nil {
			t.Fatal(err)
		}
		got[id] = r
	}
	rows.Close()

	want := map[int]row{
		1: {50000, "2026-09-01"},  // balance before the earliest marked month, dated then
		2: {900000, "2026-07-04"}, // untouched; start date from created_at
		3: {40000, "2026-09-01"},
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("debt %d = %+v, want %+v", id, got[id], w)
		}
	}

	promos := map[int]int{}
	rows, err = db.Query(`SELECT id, remaining_cents FROM promo_items`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id, rem int
		if err := rows.Scan(&id, &rem); err != nil {
			t.Fatal(err)
		}
		promos[id] = rem
	}
	rows.Close()
	// Purchases go back to what they were before the earliest month; one
	// not listed in items_before keeps its amount.
	if promos[10] != 20000 || promos[11] != 20000 || promos[12] != 7000 {
		t.Errorf("promo items = %v, want 10:20000 11:20000 12:7000", promos)
	}
}

func TestMigration012ConvertsBudgetToExtra(t *testing.T) {
	setup := func(t *testing.T, budget int) *sql.DB {
		db := openRaw(t)
		migrateThrough(t, db, 11)
		mustExec(t, db, `INSERT INTO debts (name, balance_cents, apr, minimum_payment_cents, inactive) VALUES
			('Visa', 450000, 24.99, 12000, 0),
			('Store', 30000, 18, 3500, 0),
			('Paused', 50000, 10, 1000, 1),  -- inactive: its minimum doesn't count
			('Done', 0, 10, 2000, 0)          -- paid off: its minimum doesn't count`)
		mustExec(t, db, `UPDATE settings SET monthly_budget_cents = ?`, budget)
		if err := migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		return db
	}
	read := func(t *testing.T, db *sql.DB) (budget, extra int) {
		if err := db.QueryRow(`SELECT monthly_budget_cents, extra_cents FROM settings`).Scan(&budget, &extra); err != nil {
			t.Fatal(err)
		}
		return
	}

	tests := []struct {
		name       string
		budget     int
		wantBudget int
		wantExtra  int
	}{
		{"above minimums becomes extra", 50000, 0, 50000 - 15500},
		{"exactly the minimums becomes zero extra", 15500, 0, 0},
		{"below minimums stays a fixed total", 10000, 10000, 0},
		{"no budget stays unset", 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setup(t, tt.budget)
			budget, extra := read(t, db)
			if budget != tt.wantBudget || extra != tt.wantExtra {
				t.Errorf("budget %d extra %d, want budget %d extra %d", budget, extra, tt.wantBudget, tt.wantExtra)
			}
		})
	}
}

func TestCheckConstraints(t *testing.T) {
	db, err := OpenPath(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mustExec(t, db, `INSERT INTO debts (id, name, balance_cents, apr, minimum_payment_cents) VALUES (1, 'Visa', 1000, 10, 100)`)

	tests := []struct {
		name string
		q    string
		ok   bool
	}{
		{"theme light", `UPDATE settings SET theme = 'light'`, true},
		{"theme dark", `UPDATE settings SET theme = 'dark'`, true},
		{"theme other", `UPDATE settings SET theme = 'blue'`, false},
		{"notice 0", `UPDATE settings SET notice_seconds = 0`, true},
		{"notice 60", `UPDATE settings SET notice_seconds = 60`, true},
		{"notice -1", `UPDATE settings SET notice_seconds = -1`, false},
		{"notice 61", `UPDATE settings SET notice_seconds = 61`, false},
		{"expand_current 2", `UPDATE settings SET expand_current = 2`, false},
		{"extra negative", `UPDATE settings SET extra_cents = -1`, false},
		{"budget negative", `UPDATE settings SET monthly_budget_cents = -1`, false},
		{"second settings row", `INSERT INTO settings (id) VALUES (2)`, false},
		{"inactive 1", `UPDATE debts SET inactive = 1`, true},
		{"inactive 0", `UPDATE debts SET inactive = 0`, true},
		{"inactive 2", `UPDATE debts SET inactive = 2`, false},
		{"zero balance allowed", `UPDATE debts SET balance_cents = 0`, true},
		{"negative balance", `UPDATE debts SET balance_cents = -1`, false},
		{"negative apr", `UPDATE debts SET apr = -0.5`, false},
		{"zero minimum", `UPDATE debts SET minimum_payment_cents = 0`, false},
		{"due day 32", `UPDATE debts SET due_day = 32`, false},
		{"due day 0", `UPDATE debts SET due_day = 0`, true},
		{"plan status bogus", `INSERT INTO plan_months (month, status) VALUES ('2026-01-01', 'done')`, false},
		{"plan negative override", `INSERT INTO plan_months (month, override_cents) VALUES ('2026-02-01', -1)`, false},
		{"plan negative extra", `INSERT INTO plan_months (month, extra_cents) VALUES ('2026-03-01', -1)`, false},
		{"promo negative remaining", `INSERT INTO promo_items (debt_id, remaining_cents, ends) VALUES (1, -1, '2027-01-01')`, false},
		{"promo unknown debt (foreign key)", `INSERT INTO promo_items (debt_id, remaining_cents, ends) VALUES (999, 1, '2027-01-01')`, false},
		{"negative budget change", `INSERT INTO plan_budget_changes (month, amount_cents) VALUES ('2026-01-01', -1)`, false},
		{"negative allocation", `INSERT INTO plan_month_allocations (month, debt_id, amount_cents) VALUES ('2026-01-01', 1, -1)`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := db.Exec(tt.q)
			if tt.ok && err != nil {
				t.Errorf("want accepted, got %v", err)
			}
			if !tt.ok && err == nil {
				t.Error("want rejected, got accepted")
			}
		})
	}
}

func TestPromoItemsCascadeOnDebtDelete(t *testing.T) {
	db, err := OpenPath(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mustExec(t, db, `INSERT INTO debts (id, name, balance_cents, apr, minimum_payment_cents) VALUES (1, 'Store', 1000, 0, 100)`)
	mustExec(t, db, `INSERT INTO promo_items (debt_id, remaining_cents, ends) VALUES (1, 1000, '2027-01-01')`)
	mustExec(t, db, `DELETE FROM debts WHERE id = 1`)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM promo_items`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("promo items left after deleting their debt: %d", n)
	}
}

func TestOpenPathFailsWhenDirCannotBeCreated(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if db, err := OpenPath(filepath.Join(blocker, "sub", "test.db")); err == nil {
		db.Close()
		t.Fatal("want an error when the db directory can't be created")
	}
}
