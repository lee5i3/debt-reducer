package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/lee5i3/debt-reducer/internal/debt"
)

// PlanRepository stores the months of the saved plan.
type PlanRepository interface {
	// Months returns every stored month, oldest first, with what each debt got.
	Months(ctx context.Context) ([]debt.MonthRecord, error)
	// SetOverride stores (or with nil clears) an edited payment for a month.
	SetOverride(ctx context.Context, month time.Time, amount *debt.Money) error
	// Record marks a month paid or skipped with what each debt got. It does
	// not change the debts; what's left is worked out from these records.
	Record(ctx context.Context, rec debt.MonthRecord) error
	// Undo un-marks a month.
	Undo(ctx context.Context, month time.Time) error
	// SetExtra stores (or with nil removes) a one-time payment for a month.
	SetExtra(ctx context.Context, month time.Time, amount *debt.Money) error
	// BudgetChanges lists scheduled monthly amounts, earliest first.
	BudgetChanges(ctx context.Context) ([]debt.BudgetChange, error)
	// SetBudgetChanges replaces the scheduled monthly amounts.
	SetBudgetChanges(ctx context.Context, changes []debt.BudgetChange) error
	// Allocations returns each edited month's per-account amounts.
	Allocations(ctx context.Context) (map[time.Time]map[int64]debt.Money, error)
	// SetAllocations replaces (or with nil clears) a month's per-account amounts.
	SetAllocations(ctx context.Context, month time.Time, amounts map[int64]debt.Money) error
}

type planRepo struct{ db *sql.DB }

// NewPlanRepository is a dig constructor for PlanRepository.
func NewPlanRepository(db *sql.DB) PlanRepository { return &planRepo{db: db} }

func monthKey(t time.Time) string { return t.Format(time.DateOnly) }

func (r *planRepo) Months(ctx context.Context) ([]debt.MonthRecord, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT month, status, override_cents, extra_cents, paid_cents, interest_cents FROM plan_months ORDER BY month`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []debt.MonthRecord
	index := map[string]int{}
	for rows.Next() {
		var key string
		var override, extra sql.NullInt64
		var m debt.MonthRecord
		if err := rows.Scan(&key, &m.Status, &override, &extra, &m.Paid, &m.Interest); err != nil {
			return nil, err
		}
		if m.Month, err = parseDate(key); err != nil {
			return nil, err
		}
		if override.Valid {
			v := debt.Money(override.Int64)
			m.Override = &v
		}
		if extra.Valid {
			v := debt.Money(extra.Int64)
			m.Extra = &v
		}
		index[key] = len(out)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	drows, err := r.db.QueryContext(ctx,
		`SELECT month, debt_id, debt_name, before_cents, interest_cents, paid_cents, minimum_cents, left_cents, role
		   FROM plan_month_debts ORDER BY month, rowid`)
	if err != nil {
		return nil, err
	}
	defer drows.Close()
	for drows.Next() {
		var key string
		var d debt.MonthDebt
		if err := drows.Scan(&key, &d.DebtID, &d.Name, &d.Before, &d.Interest, &d.Paid, &d.Minimum, &d.Left, &d.Role); err != nil {
			return nil, err
		}
		if i, ok := index[key]; ok {
			out[i].Debts = append(out[i].Debts, d)
		}
	}
	return out, drows.Err()
}

func (r *planRepo) SetOverride(ctx context.Context, month time.Time, amount *debt.Money) error {
	if amount == nil {
		if _, err := r.db.ExecContext(ctx, `UPDATE plan_months SET override_cents = NULL WHERE month = ?`, monthKey(month)); err != nil {
			return err
		}
		return r.dropEmpty(ctx, month)
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO plan_months (month, override_cents) VALUES (?, ?)
		 ON CONFLICT (month) DO UPDATE SET override_cents = excluded.override_cents`,
		monthKey(month), *amount)
	return err
}

func (r *planRepo) Record(ctx context.Context, rec debt.MonthRecord) error {
	return r.inTx(ctx, func(tx *sql.Tx) error {
		key := monthKey(rec.Month)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO plan_months (month, status, paid_cents, interest_cents, marked_at)
			 VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
			 ON CONFLICT (month) DO UPDATE SET status = excluded.status, paid_cents = excluded.paid_cents,
			     interest_cents = excluded.interest_cents, marked_at = excluded.marked_at`,
			key, rec.Status, rec.Paid, rec.Interest); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM plan_month_debts WHERE month = ?`, key); err != nil {
			return err
		}
		for _, d := range rec.Debts {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO plan_month_debts (month, debt_id, debt_name, before_cents, interest_cents, paid_cents, minimum_cents, left_cents, role)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				key, d.DebtID, d.Name, d.Before, d.Interest, d.Paid, min(d.Minimum, d.Paid), d.Left, d.Role); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *planRepo) Undo(ctx context.Context, month time.Time) error {
	return r.inTx(ctx, func(tx *sql.Tx) error {
		key := monthKey(month)
		if _, err := tx.ExecContext(ctx, `DELETE FROM plan_month_debts WHERE month = ?`, key); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE plan_months SET status = '', paid_cents = 0, interest_cents = 0, marked_at = NULL WHERE month = ?`,
			key); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`DELETE FROM plan_months WHERE month = ? AND override_cents IS NULL AND extra_cents IS NULL`, key)
		return err
	})
}

func (r *planRepo) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	return inTx(ctx, r.db, fn)
}

func inTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (r *planRepo) SetExtra(ctx context.Context, month time.Time, amount *debt.Money) error {
	if amount == nil {
		if _, err := r.db.ExecContext(ctx, `UPDATE plan_months SET extra_cents = NULL WHERE month = ?`, monthKey(month)); err != nil {
			return err
		}
		return r.dropEmpty(ctx, month)
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO plan_months (month, extra_cents) VALUES (?, ?)
		 ON CONFLICT (month) DO UPDATE SET extra_cents = excluded.extra_cents`,
		monthKey(month), *amount)
	return err
}

// dropEmpty removes a month row that no longer holds anything.
func (r *planRepo) dropEmpty(ctx context.Context, month time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM plan_months WHERE month = ? AND status = '' AND override_cents IS NULL AND extra_cents IS NULL`,
		monthKey(month))
	return err
}

func (r *planRepo) BudgetChanges(ctx context.Context) ([]debt.BudgetChange, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT month, amount_cents FROM plan_budget_changes ORDER BY month`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []debt.BudgetChange
	for rows.Next() {
		var key string
		var c debt.BudgetChange
		if err := rows.Scan(&key, &c.Amount); err != nil {
			return nil, err
		}
		if c.Month, err = parseDate(key); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *planRepo) SetBudgetChanges(ctx context.Context, changes []debt.BudgetChange) error {
	return r.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM plan_budget_changes`); err != nil {
			return err
		}
		for _, c := range changes {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO plan_budget_changes (month, amount_cents) VALUES (?, ?)`,
				monthKey(c.Month), c.Amount); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *planRepo) Allocations(ctx context.Context) (map[time.Time]map[int64]debt.Money, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT month, debt_id, amount_cents FROM plan_month_allocations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[time.Time]map[int64]debt.Money{}
	for rows.Next() {
		var key string
		var id int64
		var amt debt.Money
		if err := rows.Scan(&key, &id, &amt); err != nil {
			return nil, err
		}
		m, err := parseDate(key)
		if err != nil {
			return nil, err
		}
		if out[m] == nil {
			out[m] = map[int64]debt.Money{}
		}
		out[m][id] = amt
	}
	return out, rows.Err()
}

func (r *planRepo) SetAllocations(ctx context.Context, month time.Time, amounts map[int64]debt.Money) error {
	return r.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM plan_month_allocations WHERE month = ?`, monthKey(month)); err != nil {
			return err
		}
		for id, amt := range amounts {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO plan_month_allocations (month, debt_id, amount_cents) VALUES (?, ?, ?)`,
				monthKey(month), id, amt); err != nil {
				return err
			}
		}
		return nil
	})
}
