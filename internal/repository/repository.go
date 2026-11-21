// Package repository persists debts and plan settings in SQLite.
package repository

import (
	"context"
	"database/sql"
	"errors"

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
}

// SettingsRepository stores the plan defaults.
type SettingsRepository interface {
	Get(ctx context.Context) (debt.Settings, error)
	Save(ctx context.Context, s debt.Settings) error
}

type debtRepo struct{ db *sql.DB }

// NewDebtRepository is a dig constructor for DebtRepository.
func NewDebtRepository(db *sql.DB) DebtRepository { return &debtRepo{db: db} }

const debtColumns = `id, name, balance_cents, apr, minimum_payment_cents, due_day`

type scanner interface{ Scan(dest ...any) error }

func scanDebt(s scanner) (debt.Debt, error) {
	var d debt.Debt
	err := s.Scan(&d.ID, &d.Name, &d.Balance, &d.APR, &d.MinimumPayment, &d.DueDay)
	return d, err
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
	return debts, rows.Err()
}

func (r *debtRepo) Get(ctx context.Context, id int64) (debt.Debt, error) {
	d, err := scanDebt(r.db.QueryRowContext(ctx, `SELECT `+debtColumns+` FROM debts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	return d, err
}

func (r *debtRepo) Create(ctx context.Context, d debt.Debt) (debt.Debt, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO debts (name, balance_cents, apr, minimum_payment_cents, due_day) VALUES (?, ?, ?, ?, ?)`,
		d.Name, d.Balance, d.APR, d.MinimumPayment, d.DueDay)
	if err != nil {
		return d, err
	}
	d.ID, err = res.LastInsertId()
	return d, err
}

func (r *debtRepo) Update(ctx context.Context, d debt.Debt) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE debts SET name = ?, balance_cents = ?, apr = ?, minimum_payment_cents = ?, due_day = ?,
		        updated_at = CURRENT_TIMESTAMP
		  WHERE id = ?`,
		d.Name, d.Balance, d.APR, d.MinimumPayment, d.DueDay, d.ID)
	if err != nil {
		return err
	}
	return requireRow(res)
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
	err := r.db.QueryRowContext(ctx, `SELECT monthly_budget_cents, strategy FROM settings WHERE id = 1`).
		Scan(&s.MonthlyBudget, &s.Strategy)
	return s, err
}

func (r *settingsRepo) Save(ctx context.Context, s debt.Settings) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE settings SET monthly_budget_cents = ?, strategy = ? WHERE id = 1`,
		s.MonthlyBudget, string(s.Strategy))
	return err
}
