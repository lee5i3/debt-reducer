package handler_test

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/lee5i3/debt-reducer/internal/debt"
)

func (e *env) settings() debt.Settings {
	e.t.Helper()
	s, err := e.debts.Settings(e.ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

func (e *env) setupPlan(extra float64) {
	e.t.Helper()
	if err := e.plans.Setup(e.ctx, debt.Avalanche, debt.Order{}, 0, debt.FromDollars(extra), thisMonth()); err != nil {
		e.t.Fatal(err)
	}
}

func TestPlanPageBeforeSetup(t *testing.T) {
	e := newEnv(t)
	e.addDebt("Visa", 4500, 120, 24.99)
	rec := e.get("/")
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "<h1>Set up your plan</h1>", `action="/plan/setup"`)
}

func TestPlanSetupMinimum(t *testing.T) {
	e := newEnv(t)
	e.addDebt("Visa", 4500, 120, 24.99)
	form := url.Values{"strategy": {"avalanche"}, "pay": {"min"}, "extra": {"300"}, "start": {monthParam(thisMonth())}}
	u := wantRedirect(t, e.postForm("/plan/setup", form))
	if u.String() != "/?msg=plan" {
		t.Fatalf("Location = %s, want /?msg=plan", u)
	}
	s := e.settings()
	if s.MonthlyBudget != 0 || s.Extra != 0 || s.Strategy != debt.Avalanche {
		t.Fatalf("settings after pay=min = %+v, want budget 0 extra 0", s)
	}
	if s.PlanStart == nil || !s.PlanStart.Equal(thisMonth()) {
		t.Fatalf("plan start = %v, want %v", s.PlanStart, thisMonth())
	}

	rec := e.get("/?msg=plan")
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "<h1>Payoff plan</h1>", "Plan saved.", "Month by month")
}

func TestPlanSetupExtra(t *testing.T) {
	e := newEnv(t)
	e.addDebt("Visa", 4500, 120, 24.99)
	form := url.Values{"strategy": {"snowball"}, "pay": {"extra"}, "extra": {"200"}, "start": {monthParam(thisMonth())}}
	wantRedirect(t, e.postForm("/plan/setup", form))
	s := e.settings()
	if s.MonthlyBudget != 0 || s.Extra != debt.FromDollars(200) || s.Strategy != debt.Snowball {
		t.Fatalf("settings after pay=extra = %+v, want extra $200", s)
	}

	// The setup dialog is filled in from the saved plan.
	rec := e.get("/plan/setup")
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, `id="setup-title"`, `value="200.00"`)
}

func TestPlanSetupInvalid(t *testing.T) {
	e := newEnv(t)
	e.addDebt("Visa", 4500, 120, 24.99)
	start := monthParam(thisMonth())
	tests := []struct {
		name string
		form url.Values
		want string
	}{
		{"zero extra", url.Values{"strategy": {"avalanche"}, "pay": {"extra"}, "extra": {"0"}, "start": {start}}, "enter the extra as a number above 0"},
		{"text extra", url.Values{"strategy": {"avalanche"}, "pay": {"extra"}, "extra": {"lots"}, "start": {start}}, "enter the extra as a number above 0"},
		{"bad start", url.Values{"strategy": {"avalanche"}, "pay": {"min"}, "start": {"soon"}}, "pick the first month of your plan"},
		{"bad strategy", url.Values{"strategy": {"yolo"}, "pay": {"min"}, "start": {start}}, "unknown strategy"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := e.postForm("/plan/setup", tc.form)
			wantStatus(t, rec, http.StatusBadRequest)
			wantBody(t, rec, tc.want)
		})
	}
	if e.settings().PlanStart != nil {
		t.Fatal("invalid setup saved a plan")
	}

	// After setup, an invalid resubmit re-opens the setup dialog.
	e.setupPlan(0)
	rec := e.postForm("/plan/setup", url.Values{"strategy": {"avalanche"}, "pay": {"extra"}, "extra": {"-5"}, "start": {start}})
	wantStatus(t, rec, http.StatusBadRequest)
	wantBody(t, rec, "enter the extra as a number above 0", `id="setup-title"`)
}

func TestMarkMonthPaidAndUndo(t *testing.T) {
	e := newEnv(t)
	visa := e.addDebt("Visa", 4500, 120, 24.99)
	e.setupPlan(100)
	month := thisMonth()
	m := monthParam(month)

	u := wantRedirect(t, e.postForm("/plan/months/"+m+"/paid", nil))
	if u.Path != "/" || u.Query().Get("msg") != "paid" || u.Query().Get("m") != m {
		t.Fatalf("Location = %s", u)
	}
	tp, err := e.plans.Tracked(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tp.MarkedCount != 1 || tp.Months[0].Status != debt.MonthPaid {
		t.Fatalf("after paid: marked=%d months[0]=%+v", tp.MarkedCount, tp.Months[0])
	}
	left, _ := e.plans.Remaining(e.ctx)
	if left[visa.ID].Balance >= debt.FromDollars(4500) {
		t.Fatalf("balance after paying = %s, want below $4,500", left[visa.ID].Balance)
	}
	rec := e.get(u.String())
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, month.Format("January 2006")+" marked paid.")

	u = wantRedirect(t, e.postForm("/plan/months/"+m+"/undo", nil))
	if u.Query().Get("msg") != "undo" {
		t.Fatalf("Location = %s", u)
	}
	tp, _ = e.plans.Tracked(e.ctx)
	if tp.MarkedCount != 0 {
		t.Fatalf("after undo: marked=%d, want 0", tp.MarkedCount)
	}
	left, _ = e.plans.Remaining(e.ctx)
	if left[visa.ID].Balance != debt.FromDollars(4500) {
		t.Fatalf("balance after undo = %s, want $4,500.00", left[visa.ID].Balance)
	}
}

func TestMarkMonthSkip(t *testing.T) {
	e := newEnv(t)
	e.addDebt("Visa", 4500, 120, 24.99)
	e.setupPlan(0)
	m := monthParam(thisMonth())
	u := wantRedirect(t, e.postForm("/plan/months/"+m+"/skip", nil))
	if u.Query().Get("msg") != "skip" {
		t.Fatalf("Location = %s", u)
	}
	tp, _ := e.plans.Tracked(e.ctx)
	if tp.MarkedCount != 1 || tp.Months[0].Status != debt.MonthSkipped {
		t.Fatalf("after skip: %+v", tp.Months[0])
	}
	wantBody(t, e.get(u.String()), "skipped. Interest added")
}

func TestMarkMonthErrors(t *testing.T) {
	e := newEnv(t)
	e.addDebt("Visa", 4500, 120, 24.99)

	// Before setup.
	rec := e.postForm("/plan/months/"+monthParam(thisMonth())+"/paid", nil)
	wantStatus(t, rec, http.StatusBadRequest)
	wantBody(t, rec, "set up your plan first")

	e.setupPlan(0)
	later := thisMonth().AddDate(0, 2, 0)
	rec = e.postForm("/plan/months/"+monthParam(later)+"/paid", nil)
	wantStatus(t, rec, http.StatusBadRequest)
	wantBody(t, rec, "mark "+thisMonth().Format("January 2006")+" first")

	// Nothing marked yet: undo fails.
	rec = e.postForm("/plan/months/"+monthParam(thisMonth())+"/undo", nil)
	wantStatus(t, rec, http.StatusBadRequest)
	wantBody(t, rec, "only the most recently marked month can be undone")

	// Unknown action and bad month are 404s.
	wantStatus(t, e.postForm("/plan/months/"+monthParam(thisMonth())+"/explode", nil), http.StatusNotFound)
	wantStatus(t, e.postForm("/plan/months/not-a-month/paid", nil), http.StatusNotFound)

	if tp, _ := e.plans.Tracked(e.ctx); tp.MarkedCount != 0 {
		t.Fatalf("errors marked %d months", tp.MarkedCount)
	}
}

func TestEditMonth(t *testing.T) {
	e := newEnv(t)
	visa := e.addDebt("Visa", 4500, 120, 24.99)
	store := e.addDebt("Store", 300, 35, 18)
	e.setupPlan(0)
	m := monthParam(thisMonth())
	path := "/plan/months/" + m + "/edit"

	rec := e.get(path)
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, `id="edit-pay-title"`, thisMonth().Format("January 2006")+" payment", "Visa", "Store",
		`name="choice_`+strconv.FormatInt(visa.ID, 10)+`"`)

	before, _ := e.plans.Tracked(e.ctx)
	basePaid := before.Months[0].Paid

	form := url.Values{
		"choice_" + strconv.FormatInt(visa.ID, 10):  {"extra"},
		"extra_" + strconv.FormatInt(visa.ID, 10):   {"50"},
		"choice_" + strconv.FormatInt(store.ID, 10): {"min"},
	}
	u := wantRedirect(t, e.postForm(path, form))
	if u.Query().Get("msg") != "edited" || u.Query().Get("m") != m || u.Query().Get("amount") != "$205.00" {
		t.Fatalf("Location = %s, want msg=edited amount=$205.00", u)
	}
	after, _ := e.plans.Tracked(e.ctx)
	if !after.Months[0].Edited || after.Months[0].Paid != basePaid+debt.FromDollars(50) {
		t.Fatalf("edited month = %+v, want paid %s", after.Months[0], basePaid+debt.FromDollars(50))
	}
	wantBody(t, e.get(u.String()), "payment changed to $205.00.")

	// An invalid extra re-shows the dialog.
	form.Set("extra_"+strconv.FormatInt(visa.ID, 10), "abc")
	rec = e.postForm(path, form)
	wantStatus(t, rec, http.StatusBadRequest)
	wantBody(t, rec, "Enter the extra as a number above 0, or choose Minimum.")

	// Reset puts the month back to the plan.
	u = wantRedirect(t, e.postForm(path, url.Values{"reset": {"1"}}))
	if u.Query().Get("msg") != "reset" {
		t.Fatalf("Location = %s", u)
	}
	after, _ = e.plans.Tracked(e.ctx)
	if after.Months[0].Edited || after.Months[0].Paid != basePaid {
		t.Fatalf("after reset = %+v, want paid %s", after.Months[0], basePaid)
	}
}

func TestEditMonthNotUpcoming(t *testing.T) {
	e := newEnv(t)
	e.addDebt("Visa", 4500, 120, 24.99)
	e.setupPlan(0)

	rec := e.get("/plan/months/1990-01/edit")
	wantStatus(t, rec, http.StatusNotFound)
	wantBody(t, rec, "January 1990 isn")
	wantStatus(t, e.get("/plan/months/bogus/edit"), http.StatusNotFound)

	// Resetting a marked month fails.
	m := monthParam(thisMonth())
	wantRedirect(t, e.postForm("/plan/months/"+m+"/paid", nil))
	rec = e.postForm("/plan/months/"+m+"/edit", url.Values{"reset": {"1"}})
	wantStatus(t, rec, http.StatusBadRequest)
	wantBody(t, rec, "that month is already marked")
}

func TestPlanPageShowAll(t *testing.T) {
	e := newEnv(t)
	e.addDebt("Visa", 4500, 120, 24.99) // minimums only: many months
	e.setupPlan(0)
	tp, _ := e.plans.Tracked(e.ctx)
	if len(tp.Months) <= 12 {
		t.Fatalf("want a long plan, got %d months", len(tp.Months))
	}
	last := tp.Months[len(tp.Months)-1].Month
	lastURL := `/plan/months/` + monthParam(last) + `/edit`

	rec := e.get("/")
	wantStatus(t, rec, http.StatusOK)
	wantNoBody(t, rec, lastURL)

	rec = e.get("/?all=1")
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, lastURL)
}

// With no active debts, Plan and Simulate send you to the debt list and the
// sidebar greys them out.
func TestNoActiveDebtsStartsOnDebtList(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{"/", "/simulate", "/plan/setup", "/lump-sum", "/plan/months/" + monthParam(thisMonth()) + "/edit"} {
		rec := e.get(path)
		wantStatus(t, rec, http.StatusSeeOther)
		if loc := rec.Header().Get("Location"); loc != "/debts" {
			t.Errorf("%s: Location = %s, want /debts", path, loc)
		}
	}
	rec := e.postForm("/plan/setup", url.Values{"pay": {"min"}})
	if loc := rec.Header().Get("Location"); loc != "/debts" {
		t.Errorf("POST /plan/setup: Location = %s, want /debts", loc)
	}
	// The settings popup survives the redirect.
	if loc := e.get("/settings").Header().Get("Location"); loc != "/?settings=1" {
		t.Fatalf("/settings Location = %s", loc)
	}
	if loc := e.get("/?settings=1").Header().Get("Location"); loc != "/debts?settings=1" {
		t.Errorf("/?settings=1 Location = %s, want /debts?settings=1", loc)
	}

	rec = e.get("/debts")
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, `aria-disabled="true" title="Add a debt to see your plan"`, `aria-disabled="true" title="Add a debt to try changes"`)
	wantNoBody(t, rec, `href="/simulate"`)

	// An inactive debt alone still leaves nothing to plan.
	d := e.addDebt("Visa", 4500, 120, 24.99)
	wantStatus(t, e.postForm("/debts/"+strconv.FormatInt(d.ID, 10)+"/active", url.Values{"active": {"0"}}), http.StatusSeeOther)
	if loc := e.get("/").Header().Get("Location"); loc != "/debts" {
		t.Errorf("inactive only: Location = %s, want /debts", loc)
	}

	wantStatus(t, e.postForm("/debts/"+strconv.FormatInt(d.ID, 10)+"/active", url.Values{"active": {"1"}}), http.StatusSeeOther)
	wantStatus(t, e.get("/"), http.StatusOK)
	rec = e.get("/debts")
	wantBody(t, rec, `href="/simulate"`)
	wantNoBody(t, rec, `aria-disabled="true"`)
}
