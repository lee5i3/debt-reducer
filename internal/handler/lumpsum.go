package handler

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/lee5i3/debt-reducer/internal/debt"
	"github.com/lee5i3/debt-reducer/internal/service"
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

// LumpSumPage moved into Simulate; keep old links working.
func (h *Handler) LumpSumPage(w http.ResponseWriter, r *http.Request) {
	q := url.Values{"mode": {"one"}}
	if a := r.URL.Query().Get("amount"); a != "" {
		q.Set("amount", a)
	}
	http.Redirect(w, r, "/simulate?"+q.Encode(), http.StatusMovedPermanently)
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
