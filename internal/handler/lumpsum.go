package handler

import (
	"fmt"
	"net/http"

	"github.com/lee5i3/debt-reducer/internal/debt"
	"github.com/lee5i3/debt-reducer/internal/service"
	"github.com/lee5i3/debt-reducer/web/views"
)

// parseAmount reads the one-time payment amount. Empty means "not entered".
func parseAmount(r *http.Request) (debt.Money, error) {
	raw := r.URL.Query().Get("amount")
	if raw == "" {
		return 0, nil
	}
	m, err := debt.ParseDollars(raw)
	if err != nil {
		return 0, err
	}
	if m <= 0 {
		return 0, debt.ErrInvalidAmount
	}
	return m, nil
}

// LumpSumPage shows where a one-time payment does the most good.
//
//	GET /lump-sum?amount=5000&strategy=&budget=&start=
func (h *Handler) LumpSumPage(w http.ResponseWriter, r *http.Request) {
	opts, optErr := planOptions(r)
	if optErr != nil {
		opts = service.Options{}
	}
	amount, amtErr := parseAmount(r)

	res, err := h.planner.LumpSum(r.Context(), amount, opts)
	if res == nil {
		h.render(w, r, statusFor(err), views.ErrorPage(h.userMessage(err)))
		return
	}

	data := views.LumpSumData{
		Debts:         res.Debts,
		Amount:        r.URL.Query().Get("amount"),
		Strategy:      res.Options.Strategy,
		MonthlyBudget: res.Options.MonthlyBudget,
		StartDate:     res.Options.StartDate,
		Analysis:      res.Analysis,
	}
	for _, d := range res.Debts {
		data.TotalBalance += d.Balance
	}
	switch {
	case optErr != nil:
		data.Error = optErr.Error()
	case amtErr != nil:
		data.Error = amtErr.Error()
	case err != nil:
		data.Error = h.userMessage(err)
	}
	h.render(w, r, http.StatusOK, views.LumpSum(data))
}

// APILumpSum handles GET /api/lump-sum?amount=&strategy=&budget=&start=.
func (h *Handler) APILumpSum(w http.ResponseWriter, r *http.Request) {
	opts, err := planOptions(r)
	if err != nil {
		h.writeError(w, fmt.Errorf("%w: %w", service.ErrInvalid, err))
		return
	}
	amount, err := parseAmount(r)
	if err == nil && amount == 0 {
		err = debt.ErrInvalidAmount
	}
	if err != nil {
		h.writeError(w, fmt.Errorf("%w: %w", service.ErrInvalid, err))
		return
	}
	res, err := h.planner.LumpSum(r.Context(), amount, opts)
	if err == nil && res.Analysis == nil {
		err = debt.ErrNoDebts
	}
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res.Analysis)
}
