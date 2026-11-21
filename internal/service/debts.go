// Package service holds the application's use cases: managing debts and
// building payoff plans.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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
	// SetActive puts a debt in the plan and Simulate, or leaves it out.
	SetActive(ctx context.Context, id int64, active bool) error
	Settings(ctx context.Context) (debt.Settings, error)
	SaveSettings(ctx context.Context, s debt.Settings) error
	Preferences(ctx context.Context) (debt.Preferences, error)
	SavePreferences(ctx context.Context, p debt.Preferences) error
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

// normalize trims input and fills defaults before validation.
func normalize(d *debt.Debt) {
	d.Name = strings.TrimSpace(d.Name)
	if d.StartDate.IsZero() {
		now := time.Now()
		d.StartDate = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	}
	if d.Type == "" {
		d.Type = debt.DefaultType
	}
	if d.Type != debt.TypePromo {
		d.Promos = nil
		return
	}
	// A promotional account's balance is what remains on its purchases; it
	// is interest-free while they run and its term ends with the last one.
	if len(d.Promos) > 0 {
		d.Balance = d.PromoTotal()
		d.APR = 0
		last := d.Promos[0].Ends
		for _, p := range d.Promos {
			if p.Ends.After(last) {
				last = p.Ends
			}
		}
		d.TermEnds = &last
	}
}

func (s *debtService) Create(ctx context.Context, d debt.Debt) (debt.Debt, error) {
	normalize(&d)
	if err := d.Validate(); err != nil {
		return d, invalid(err)
	}
	return s.debts.Create(ctx, d)
}

func (s *debtService) Update(ctx context.Context, d debt.Debt) error {
	normalize(&d)
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

func (s *debtService) Preferences(ctx context.Context) (debt.Preferences, error) {
	return s.settings.Preferences(ctx)
}

func (s *debtService) SavePreferences(ctx context.Context, p debt.Preferences) error {
	if !p.Theme.Valid() {
		return fmt.Errorf("%w: theme must be dark or light", ErrInvalid)
	}
	if p.NoticeSeconds < 0 || p.NoticeSeconds > 60 {
		return fmt.Errorf("%w: close banners after 0 to 60 seconds", ErrInvalid)
	}
	return s.settings.SavePreferences(ctx, p)
}

func (s *debtService) SetActive(ctx context.Context, id int64, active bool) error {
	return s.debts.SetInactive(ctx, id, !active)
}
