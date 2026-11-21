// Package service holds the application's use cases: managing debts and
// building payoff plans.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/lee5i3/debt-reducer/internal/debt"
	"github.com/lee5i3/debt-reducer/internal/repository"
)

var (
	// ErrInvalid wraps user input validation failures.
	ErrInvalid = errors.New("invalid input")
	// ErrNotFound is returned when a debt does not exist.
	ErrNotFound = repository.ErrNotFound
)

func invalid(err error) error { return fmt.Errorf("%w: %w", ErrInvalid, err) }

// DebtService manages the user's debts and plan settings.
type DebtService interface {
	List(ctx context.Context) ([]debt.Debt, error)
	Get(ctx context.Context, id int64) (debt.Debt, error)
	Create(ctx context.Context, d debt.Debt) (debt.Debt, error)
	Update(ctx context.Context, d debt.Debt) error
	Delete(ctx context.Context, id int64) error
	Settings(ctx context.Context) (debt.Settings, error)
	SaveSettings(ctx context.Context, s debt.Settings) error
}

type debtService struct {
	debts    repository.DebtRepository
	settings repository.SettingsRepository
}

// NewDebtService is a dig constructor for DebtService.
func NewDebtService(debts repository.DebtRepository, settings repository.SettingsRepository) DebtService {
	return &debtService{debts: debts, settings: settings}
}

func (s *debtService) List(ctx context.Context) ([]debt.Debt, error) { return s.debts.List(ctx) }

func (s *debtService) Get(ctx context.Context, id int64) (debt.Debt, error) {
	return s.debts.Get(ctx, id)
}

func (s *debtService) Create(ctx context.Context, d debt.Debt) (debt.Debt, error) {
	d.Name = strings.TrimSpace(d.Name)
	if err := d.Validate(); err != nil {
		return d, invalid(err)
	}
	return s.debts.Create(ctx, d)
}

func (s *debtService) Update(ctx context.Context, d debt.Debt) error {
	d.Name = strings.TrimSpace(d.Name)
	if err := d.Validate(); err != nil {
		return invalid(err)
	}
	return s.debts.Update(ctx, d)
}

func (s *debtService) Delete(ctx context.Context, id int64) error { return s.debts.Delete(ctx, id) }

func (s *debtService) Settings(ctx context.Context) (debt.Settings, error) {
	return s.settings.Get(ctx)
}

func (s *debtService) SaveSettings(ctx context.Context, set debt.Settings) error {
	if !set.Strategy.Valid() {
		return invalid(fmt.Errorf("unknown strategy %q", set.Strategy))
	}
	if set.MonthlyBudget < 0 {
		return invalid(errors.New("monthly budget cannot be negative"))
	}
	return s.settings.Save(ctx, set)
}
