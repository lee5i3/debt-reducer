package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/lee5i3/debt-reducer/internal/config"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lee5i3/debt-reducer/internal/debt"
	"github.com/lee5i3/debt-reducer/internal/handler"
	"github.com/lee5i3/debt-reducer/internal/repository"
	"github.com/lee5i3/debt-reducer/internal/server"
	"github.com/lee5i3/debt-reducer/internal/service"
	"github.com/lee5i3/debt-reducer/internal/storage"
)

// env is the real handler stack on an in-memory database, served through
// the real router.
type env struct {
	t      *testing.T
	router http.Handler
	debts  service.DebtService
	plans  service.PlanService
	ctx    context.Context
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := storage.OpenPath(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	debtRepo := repository.NewDebtRepository(db)
	settings := repository.NewSettingsRepository(db)
	calc := debt.NewCalculator()
	debts := service.NewDebtService(debtRepo, settings)
	planner := service.NewPlanner(debts, calc)
	plans := service.NewPlanService(debtRepo, settings, repository.NewPlanRepository(db), calc)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handler.New(debts, planner, plans, log, &config.Config{DBPath: "/data/debts/debt-reducer.db"})
	router, err := server.NewRouter(h)
	if err != nil {
		t.Fatal(err)
	}
	return &env{t: t, router: router, debts: debts, plans: plans, ctx: context.Background()}
}

// today is midnight local time today, like the handlers use.
func today() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
}

// thisMonth is the first of the current month.
func thisMonth() time.Time { return service.FirstOfMonth(time.Now()) }

func monthParam(t time.Time) string { return t.Format("2006-01") }

// addDebt stores a debt directly through the service.
func (e *env) addDebt(name string, balance, minimum float64, apr float64) debt.Debt {
	e.t.Helper()
	d, err := e.debts.Create(e.ctx, debt.Debt{
		Name: name, Type: debt.TypeCard, Balance: debt.FromDollars(balance), APR: apr,
		MinimumPayment: debt.FromDollars(minimum), DueDay: 15, StartDate: today(),
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return d
}

func (e *env) serve(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func (e *env) get(path string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.serve(httptest.NewRequest(http.MethodGet, path, nil))
}

func (e *env) postForm(path string, form url.Values) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return e.serve(req)
}

func (e *env) sendJSON(method, path string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		r = strings.NewReader(b)
	default:
		buf, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		r = bytes.NewReader(buf)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	return e.serve(req)
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body:\n%s", rec.Code, want, truncate(rec.Body.String()))
	}
}

// wantRedirect checks a 303 and returns the parsed Location.
func wantRedirect(t *testing.T, rec *httptest.ResponseRecorder) *url.URL {
	t.Helper()
	wantStatus(t, rec, http.StatusSeeOther)
	u, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("bad Location %q: %v", rec.Header().Get("Location"), err)
	}
	return u
}

func wantBody(t *testing.T, rec *httptest.ResponseRecorder, parts ...string) {
	t.Helper()
	body := rec.Body.String()
	for _, p := range parts {
		if !strings.Contains(body, p) {
			t.Errorf("body missing %q; body:\n%s", p, truncate(body))
		}
	}
}

func wantNoBody(t *testing.T, rec *httptest.ResponseRecorder, parts ...string) {
	t.Helper()
	body := rec.Body.String()
	for _, p := range parts {
		if strings.Contains(body, p) {
			t.Errorf("body should not contain %q", p)
		}
	}
}

func decodeJSON[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return v
}

func truncate(s string) string {
	if len(s) > 3000 {
		return s[:3000] + "…"
	}
	return s
}
