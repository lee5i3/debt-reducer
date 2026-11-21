// Package handler contains the HTTP handlers for the web UI and JSON API.
package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/lee5i3/debt-reducer/internal/debt"
	"github.com/lee5i3/debt-reducer/internal/service"
)

// Handler serves the UI and API.
type Handler struct {
	debts   service.DebtService
	planner service.Planner
	log     *slog.Logger
}

// New is a dig constructor for Handler.
func New(debts service.DebtService, planner service.Planner, log *slog.Logger) *Handler {
	return &Handler{debts: debts, planner: planner, log: log}
}

// Health is a liveness probe.
func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// planOptions reads strategy/budget/start overrides from the query string.
// Missing values are left zero so the saved settings are used.
func planOptions(r *http.Request) (service.Options, error) {
	var opts service.Options
	q := r.URL.Query()

	if s := q.Get("strategy"); s != "" {
		opts.Strategy = debt.Strategy(s)
		if !opts.Strategy.Valid() {
			return opts, errors.New("strategy must be one of avalanche, snowball, minimum")
		}
	}
	if b := q.Get("budget"); b != "" {
		m, err := debt.ParseDollars(b)
		if err != nil || m < 0 {
			return opts, errors.New("budget must be a positive amount")
		}
		opts.MonthlyBudget = m
	}
	if s := q.Get("start"); s != "" {
		t, err := time.ParseInLocation(time.DateOnly, s, time.Local)
		if err != nil {
			return opts, errors.New("start must be a date in YYYY-MM-DD format")
		}
		opts.StartDate = t
	}
	return opts, nil
}

func idParam(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, service.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, service.ErrInvalid), errors.Is(err, debt.ErrInvalidAmount):
		return http.StatusBadRequest
	case errors.Is(err, debt.ErrBudgetTooLow), errors.Is(err, debt.ErrNeverPaidOff), errors.Is(err, debt.ErrNoDebts):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}

// userMessage hides internal error details from responses.
func (h *Handler) userMessage(err error) string {
	if statusFor(err) == http.StatusInternalServerError {
		h.log.Error("request failed", "err", err)
		return "something went wrong"
	}
	return strings.TrimPrefix(err.Error(), service.ErrInvalid.Error()+": ")
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		h.log.Error("render", "err", err)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *Handler) writeError(w http.ResponseWriter, err error) {
	writeJSON(w, statusFor(err), map[string]string{"error": h.userMessage(err)})
}
