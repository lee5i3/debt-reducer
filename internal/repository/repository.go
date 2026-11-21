// Package repository persists debts and plan settings in SQLite.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lee5i3/debt-reducer/internal/debt"
)

// ErrNotFound is returned when a record does not exist.
var ErrNotFound = errors.New("not found")

// DebtRepository stores debts.
type DebtRepository interface {
	List(ctx context.Context) ([]debt.Debt, error)
	Get(ctx context.Context, id int64) (debt.Debt, error)
	Create(ctx context.Context, d debt.Debt) (debt.Debt, error)
	Update(ctx context.Context, d debt.Debt) error
	Delete(ctx context.Context, id int64) error
	// SetInactive leaves a debt out of (or puts it back in) the plan.
	SetInactive(ctx context.Context, id int64, inactive bool) error
}

// SettingsRepository stores the plan defaults.
type SettingsRepository interface {
	Get(ctx context.Context) (debt.Settings, error)
	// Save stores the strategy and monthly budget.
	Save(ctx context.Context, s debt.Settings) error
	// SavePlanStart stores the plan's first month and starting total.
	SavePlanStart(ctx context.Context, start time.Time, total debt.Money) error
	// Preferences and SavePreferences hold the display settings.
	Preferences(ctx context.Context) (debt.Preferences, error)
	SavePreferences(ctx context.Context, p debt.Preferences) error
}

type debtRepo struct{ db *sql.DB }

// NewDebtRepository is a dig constructor for DebtRepository.
func NewDebtRepository(db *sql.DB) DebtRepository { return &debtRepo{db: db} }

const debtColumns = `id, name, type, balance_cents, apr, minimum_payment_cents, due_day, term_ends, start_date, inactive`

type scanner interface{ Scan(dest ...any) error }

func scanDebt(s scanner) (debt.Debt, error) {
	var d debt.Debt
	var ends, start sql.NullString
	if err := s.Scan(&d.ID, &d.Name, &d.Type, &d.Balance, &d.APR, &d.MinimumPayment, &d.DueDay, &ends, &start, &d.Inactive); err != nil {
		return d, err
	}
	if start.Valid && start.String != "" {
		t, err := parseDate(start.String)
		if err != nil {
			return d, err
		}
		d.StartDate = t
	}
	if ends.Valid && ends.String != "" {
		t, err := time.ParseInLocation(time.DateOnly, ends.String, time.Local)
		if err != nil {
			return d, fmt.Errorf("debt %d: bad term_ends %q: %w", d.ID, ends.String, err)
		}
		d.TermEnds = &t
	}
	return d, nil
}

// dateOrNull stores an optional date as YYYY-MM-DD.
func dateOrNull(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(time.DateOnly)
}

func (r *debtRepo) List(ctx context.Context) ([]debt.Debt, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+debtColumns+` FROM debts ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	debts := []debt.Debt{}
	for rows.Next() {
		d, err := scanDebt(rows)
		if err != nil {
			return nil, err
		}
		debts = append(debts, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items, err := r.promoItems(ctx, 0)
	if err != nil {
		return nil, err
	}
	for i := range debts {
		debts[i].Promos = items[debts[i].ID]
	}
	return debts, nil
}

func (r *debtRepo) Get(ctx context.Context, id int64) (debt.Debt, error) {
	d, err := scanDebt(r.db.QueryRowContext(ctx, `SELECT `+debtColumns+` FROM debts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	items, err := r.promoItems(ctx, id)
	d.Promos = items[id]
	return d, err
}

// promoItems loads promotional purchases, earliest expiring first, for one
// debt (or all when debtID is 0), keyed by debt.
func (r *debtRepo) promoItems(ctx context.Context, debtID int64) (map[int64][]debt.PromoItem, error) {
	q := `SELECT id, debt_id, remaining_cents, ends FROM promo_items`
	var args []any
	if debtID != 0 {
		q += ` WHERE debt_id = ?`
		args = append(args, debtID)
	}
	rows, err := r.db.QueryContext(ctx, q+` ORDER BY ends, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]debt.PromoItem{}
	for rows.Next() {
		var p debt.PromoItem
		var owner int64
		var ends string
		if err := rows.Scan(&p.ID, &owner, &p.Remaining, &ends); err != nil {
			return nil, err
		}
		if p.Ends, err = parseDate(ends); err != nil {
			return nil, err
		}
		out[owner] = append(out[owner], p)
	}
	return out, rows.Err()
}

func (r *debtRepo) Create(ctx context.Context, d debt.Debt) (debt.Debt, error) {
	err := inTx(ctx, r.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO debts (name, type, balance_cents, apr, minimum_payment_cents, due_day, term_ends, start_date, inactive) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			d.Name, d.Type, d.Balance, d.APR, d.MinimumPayment, d.DueDay, dateOrNull(d.TermEnds), d.StartDate.Format(time.DateOnly), d.Inactive)
		if err != nil {
			return err
		}
		if d.ID, err = res.LastInsertId(); err != nil {
			return err
		}
		return savePromoItems(ctx, tx, d.ID, d.Promos)
	})
	return d, err
}

func (r *debtRepo) Update(ctx context.Context, d debt.Debt) error {
	return inTx(ctx, r.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE debts SET name = ?, type = ?, balance_cents = ?, apr = ?, minimum_payment_cents = ?, due_day = ?,
			        term_ends = ?, start_date = ?, inactive = ?, updated_at = CURRENT_TIMESTAMP
			  WHERE id = ?`,
			d.Name, d.Type, d.Balance, d.APR, d.MinimumPayment, d.DueDay, dateOrNull(d.TermEnds), d.StartDate.Format(time.DateOnly), d.Inactive, d.ID)
		if err != nil {
			return err
		}
		if err := requireRow(res); err != nil {
			return err
		}
		return savePromoItems(ctx, tx, d.ID, d.Promos)
	})
}

// savePromoItems replaces a debt's promotional purchases.
func savePromoItems(ctx context.Context, tx *sql.Tx, debtID int64, items []debt.PromoItem) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM promo_items WHERE debt_id = ?`, debtID); err != nil {
		return err
	}
	for _, p := range items {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO promo_items (debt_id, remaining_cents, ends) VALUES (?, ?, ?)`,
			debtID, p.Remaining, p.Ends.Format(time.DateOnly)); err != nil {
			return err
		}
	}
	return nil
}

func (r *debtRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM debts WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return requireRow(res)
}

func requireRow(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

type settingsRepo struct{ db *sql.DB }

// NewSettingsRepository is a dig constructor for SettingsRepository.
func NewSettingsRepository(db *sql.DB) SettingsRepository { return &settingsRepo{db: db} }

func (r *settingsRepo) Get(ctx context.Context) (debt.Settings, error) {
	var s debt.Settings
	var start sql.NullString
	err := r.db.QueryRowContext(ctx,
		`SELECT monthly_budget_cents, extra_cents, strategy, order_by, order_first, plan_start, start_total_cents FROM settings WHERE id = 1`).
		Scan(&s.MonthlyBudget, &s.Extra, &s.Strategy, &s.Order.By, &s.Order.First, &start, &s.StartTotal)
	if err != nil {
		return s, err
	}
	if start.Valid && start.String != "" {
		t, err := parseDate(start.String)
		if err != nil {
			return s, err
		}
		s.PlanStart = &t
	}
	return s, nil
}

func (r *settingsRepo) SavePlanStart(ctx context.Context, start time.Time, total debt.Money) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE settings SET plan_start = ?, start_total_cents = ? WHERE id = 1`,
		start.Format(time.DateOnly), total)
	return err
}

func parseDate(s string) (time.Time, error) {
	t, err := time.ParseInLocation(time.DateOnly, s, time.Local)
	if err != nil {
		return t, fmt.Errorf("bad date %q: %w", s, err)
	}
	return t, nil
}

func (r *settingsRepo) Save(ctx context.Context, s debt.Settings) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE settings SET monthly_budget_cents = ?, extra_cents = ?, strategy = ?, order_by = ?, order_first = ? WHERE id = 1`,
		s.MonthlyBudget, s.Extra, string(s.Strategy), s.Order.Normalize().By, s.Order.Normalize().First)
	return err
}

func (r *settingsRepo) Preferences(ctx context.Context) (debt.Preferences, error) {
	var p debt.Preferences
	err := r.db.QueryRowContext(ctx, `SELECT theme, expand_current, notice_seconds FROM settings WHERE id = 1`).
		Scan(&p.Theme, &p.ExpandCurrent, &p.NoticeSeconds)
	return p, err
}

func (r *settingsRepo) SavePreferences(ctx context.Context, p debt.Preferences) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE settings SET theme = ?, expand_current = ?, notice_seconds = ? WHERE id = 1`,
		string(p.Theme), p.ExpandCurrent, p.NoticeSeconds)
	return err
}

func (r *debtRepo) SetInactive(ctx context.Context, id int64, inactive bool) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE debts SET inactive = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, inactive, id)
	if err != nil {
		return err
	}
	return requireRow(res)
}
