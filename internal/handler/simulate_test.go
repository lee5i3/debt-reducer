package handler_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/lee5i3/debt-reducer/internal/debt"
)

func TestSimulateEmpty(t *testing.T) {
	e := newEnv(t)
	rec := e.get("/simulate")
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "<h1>Simulate</h1>")
	wantNoBody(t, rec, `action="/simulate/apply"`)
}

func TestSimulateOneTime(t *testing.T) {
	e := newEnv(t)
	e.addDebt("Visa", 4500, 120, 24.99)
	e.addDebt("Store", 300, 35, 18)
	e.setupPlan(100)

	rec := e.get("/simulate?mode=one&amount=1,000&month=0")
	wantStatus(t, rec, http.StatusOK)
	next := thisMonth()
	wantBody(t, rec, "<h1>Simulate</h1>",
		"Where your $1,000.00 goes in "+next.Format("January 2006"),
		"The extra goes to Visa first", // highest APR under avalanche
		"Includes $1,000.00 one-time payment",
		"Every payment until paid off",
		`action="/simulate/apply"`,
	)

	// Default amount, show-all rows, month clamped to the range.
	rec = e.get("/simulate?month=99&all=1")
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "Includes $5,000.00 one-time payment")
}

func TestSimulateMonthly(t *testing.T) {
	e := newEnv(t)
	e.addDebt("Visa", 4500, 120, 24.99)
	e.addDebt("Store", 300, 35, 18)
	e.setupPlan(100) // $255 a month

	rec := e.get("/simulate?mode=monthly&amount=50&month=2")
	wantStatus(t, rec, http.StatusOK)
	from := thisMonth().AddDate(0, 2, 0)
	wantBody(t, rec, "a month instead of $255.00 from "+from.Format("January 2006"), "Includes $50.00 extra")
	wantNoBody(t, rec, "Where your")
}

func TestSimulateBadAmount(t *testing.T) {
	e := newEnv(t)
	e.addDebt("Visa", 4500, 120, 24.99)
	rec := e.get("/simulate?amount=abc")
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "enter an amount above $0", `role="alert"`)
	wantNoBody(t, rec, "Every payment until paid off")
}

func TestApplySimulationOneTime(t *testing.T) {
	e := newEnv(t)
	e.addDebt("Visa", 4500, 120, 24.99)
	e.setupPlan(0)
	month := thisMonth().AddDate(0, 1, 0)

	u := wantRedirect(t, e.postForm("/simulate/apply", url.Values{"mode": {"one"}, "amount": {"250"}, "month": {"1"}}))
	if u.Path != "/" || u.Query().Get("msg") != "sim-one" || u.Query().Get("m") != monthParam(month) || u.Query().Get("amount") != "$250.00" {
		t.Fatalf("Location = %s", u)
	}
	tp, _ := e.plans.Tracked(e.ctx)
	var got debt.Money
	for _, m := range tp.Months {
		if m.Month.Equal(month) {
			got = m.Extra
		}
	}
	if got != debt.FromDollars(250) {
		t.Fatalf("one-time extra in %s = %s, want $250.00", month.Format("Jan 2006"), got)
	}
	wantBody(t, e.get(u.String()), "$250.00 one-time payment added to "+month.Format("January 2006")+".")

	// Remove it again from the month menu.
	u = wantRedirect(t, e.postForm("/plan/months/"+monthParam(month)+"/remove-extra", nil))
	if u.Query().Get("msg") != "remove-extra" {
		t.Fatalf("Location = %s", u)
	}
	tp, _ = e.plans.Tracked(e.ctx)
	for _, m := range tp.Months {
		if m.Month.Equal(month) && m.Extra != 0 {
			t.Fatalf("extra not removed: %s", m.Extra)
		}
	}
}

func TestApplySimulationMonthly(t *testing.T) {
	e := newEnv(t)
	e.addDebt("Visa", 4500, 120, 24.99)
	e.setupPlan(100)

	u := wantRedirect(t, e.postForm("/simulate/apply", url.Values{"mode": {"monthly"}, "amount": {"50"}, "month": {"0"}}))
	if u.Query().Get("msg") != "sim-monthly" || u.Query().Get("amount") != "$50.00" {
		t.Fatalf("Location = %s", u)
	}
	rec := e.get("/api/settings")
	got := decodeJSON[map[string]any](t, rec)
	if got["extra"] != 150.0 {
		t.Fatalf("extra after raising monthly by $50 = %v, want 150", got["extra"])
	}
}

func TestApplySimulationErrors(t *testing.T) {
	e := newEnv(t)
	e.addDebt("Visa", 4500, 120, 24.99)

	rec := e.postForm("/simulate/apply", url.Values{"mode": {"one"}, "amount": {"100"}})
	wantStatus(t, rec, http.StatusBadRequest)
	wantBody(t, rec, "set up your plan first")

	e.setupPlan(0)
	rec = e.postForm("/simulate/apply", url.Values{"mode": {"one"}, "amount": {"0"}})
	wantStatus(t, rec, http.StatusBadRequest)
	wantBody(t, rec, "enter an amount above $0")
}
