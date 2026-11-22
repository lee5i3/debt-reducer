// Package handler contains the HTTP handlers for the web UI and JSON API.
package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/lee5i3/debt-reducer/internal/config"
	"github.com/lee5i3/debt-reducer/internal/debt"
	"github.com/lee5i3/debt-reducer/internal/service"
	"github.com/lee5i3/debt-reducer/web/views/components"
	"github.com/lee5i3/debt-reducer/web/views/pages"
)

// Handler serves the UI and API.
type Handler struct {
	debts   service.DebtService
	planner service.Planner
	plans   service.PlanService
	log     *slog.Logger
	// dbPath is the database file, shown under Data in Settings.
	dbPath string
}

// New is a dig constructor for Handler.
func New(debts service.DebtService, planner service.Planner, plans service.PlanService, log *slog.Logger, cfg *config.Config) *Handler {
	return &Handler{debts: debts, planner: planner, plans: plans, log: log, dbPath: absPath(cfg.DBPath)}
}

// absPath is p made absolute, or p itself if that fails.
func absPath(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
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

// shell loads the sidebar and top bar data shown on every page.
func (h *Handler) shell(r *http.Request, active string) components.Shell {
	tp, err := h.plans.Tracked(r.Context())
	if err != nil {
		h.log.Warn("load sidebar summary", "err", err)
		s := h.withPreferences(r, components.Shell{Active: active})
		h.countDebts(r, &s)
		return s
	}
	return h.shellFrom(r, active, tp)
}

// countDebts fills in the sidebar's debt counts.
func (h *Handler) countDebts(r *http.Request, s *components.Shell) {
	debts, err := h.debts.List(r.Context())
	if err != nil {
		return
	}
	s.DebtCount = len(debts)
	s.ActiveDebts = countActive(debts)
}

func countActive(debts []debt.Debt) int {
	n := 0
	for _, d := range debts {
		if !d.Inactive {
			n++
		}
	}
	return n
}

// RequirePlan sends Plan and Simulate requests to the debt list while there
// are no active debts, so a new user starts by adding one.
func (h *Handler) RequirePlan(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		debts, err := h.debts.List(r.Context())
		if err == nil && countActive(debts) == 0 {
			to := "/debts"
			if r.Method == http.MethodGet && r.URL.Query().Get("settings") == "1" {
				to += "?settings=1" // keep the settings popup open
			}
			http.Redirect(w, r, to, http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// shellFrom builds the sidebar from an already-loaded plan. The debt-free
// date comes from the saved plan, or from the saved settings before setup.
func (h *Handler) shellFrom(r *http.Request, active string, tp *service.TrackedPlan) components.Shell {
	s := h.withPreferences(r, components.Shell{Active: active, TotalOwed: tp.Owed,
		InterestPaid: tp.InterestPaid(), InterestAhead: tp.InterestAhead()})
	h.countDebts(r, &s)
	if tp.Set {
		s.DebtFree = tp.DebtFree()
		return s
	}
	if res, err := h.planner.Build(r.Context(), service.Options{}); err == nil && res.Plan != nil {
		s.DebtFree = res.Plan.DebtFreeDate
	}
	return s
}

// errorPage renders a full error page inside the app shell.
func (h *Handler) errorPage(w http.ResponseWriter, r *http.Request, active string, err error) {
	h.render(w, r, statusFor(err), pages.ErrorPage(h.shell(r, active), h.userMessage(err)))
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

// preferences loads the display settings, falling back to the defaults.
func (h *Handler) preferences(r *http.Request) debt.Preferences {
	p, err := h.debts.Preferences(r.Context())
	if err != nil {
		h.log.Warn("load preferences", "err", err)
		return debt.DefaultPreferences
	}
	return p
}

// withPreferences adds the theme, the current URL and, when ?settings=1 is
// set, what the settings popup needs.
func (h *Handler) withPreferences(r *http.Request, s components.Shell) components.Shell {
	p := h.preferences(r)
	s.Theme = p.Theme
	s.NoticeSeconds = p.NoticeSeconds
	q := r.URL.Query()
	open := q.Get("settings") == "1"
	failed := q.Get("settings_err") == "1"
	// Closing settings goes back to the page without its one-time banner.
	for _, k := range []string{"settings", "settings_err", "msg", "name", "amount", "m"} {
		q.Del(k)
	}
	s.Here = r.URL.Path
	if len(q) > 0 {
		s.Here += "?" + q.Encode()
	}
	if open {
		s.Settings = &p
		s.DataFile = h.dbPath
		if failed {
			s.SettingsError = "Those settings couldn't be saved. Pick a theme and try again."
		}
	}
	return s
}
