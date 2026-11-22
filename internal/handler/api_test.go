package handler_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// apiDebt mirrors the JSON shape of a debt.
type apiDebt struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	Type           string  `json:"type"`
	Balance        float64 `json:"balance"`
	APR            float64 `json:"apr"`
	MinimumPayment float64 `json:"minimumPayment"`
	DueDay         int     `json:"dueDay"`
	StartDate      string  `json:"startDate"`
	Inactive       bool    `json:"inactive"`
}

func apiDebtPath(id int64) string { return "/api/debts/" + strconv.FormatInt(id, 10) }

func TestAPIDebtsCRUD(t *testing.T) {
	e := newEnv(t)

	rec := e.get("/api/debts")
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[[]apiDebt](t, rec); len(got) != 0 {
		t.Fatalf("empty list = %+v", got)
	}

	rec = e.sendJSON(http.MethodPost, "/api/debts",
		`{"id": 77, "name": " Visa ", "type": "Credit Card", "balance": 4500.5, "apr": 24.99, "minimumPayment": 120, "dueDay": 15}`)
	wantStatus(t, rec, http.StatusCreated)
	created := decodeJSON[apiDebt](t, rec)
	if created.ID == 0 || created.ID == 77 || created.Name != "Visa" || created.Balance != 4500.5 ||
		created.MinimumPayment != 120 || created.DueDay != 15 || created.StartDate == "" {
		t.Fatalf("created = %+v", created)
	}

	rec = e.get(apiDebtPath(created.ID))
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[apiDebt](t, rec); got.Name != "Visa" || got.Balance != 4500.5 {
		t.Fatalf("get = %+v", got)
	}

	rec = e.sendJSON(http.MethodPut, apiDebtPath(created.ID),
		map[string]any{"name": "Visa Gold", "type": "Credit Card", "balance": 4000, "apr": 19.99, "minimumPayment": 110, "inactive": true})
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[apiDebt](t, rec); got.ID != created.ID || got.Name != "Visa Gold" {
		t.Fatalf("put response = %+v", got)
	}
	d, err := e.debts.Get(e.ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "Visa Gold" || d.APR != 19.99 || !d.Inactive {
		t.Fatalf("stored after put = %+v", d)
	}

	rec = e.get("/api/debts")
	if got := decodeJSON[[]apiDebt](t, rec); len(got) != 1 || got[0].Name != "Visa Gold" {
		t.Fatalf("list = %+v", got)
	}

	rec = e.sendJSON(http.MethodDelete, apiDebtPath(created.ID), nil)
	wantStatus(t, rec, http.StatusNoContent)
	if rec.Body.Len() != 0 {
		t.Fatalf("delete body = %q", rec.Body.String())
	}
	rec = e.get(apiDebtPath(created.ID))
	wantStatus(t, rec, http.StatusNotFound)
	if got := decodeJSON[map[string]string](t, rec); got["error"] != "not found" {
		t.Fatalf("404 body = %v", got)
	}
}

func TestAPIDebtsErrors(t *testing.T) {
	e := newEnv(t)
	tests := []struct {
		name   string
		method string
		path   string
		body   any
		status int
		want   string // substring of the error message
	}{
		{"create missing name", http.MethodPost, "/api/debts", `{"balance": 100, "minimumPayment": 25}`, 400, "debt name is required"},
		{"create zero balance", http.MethodPost, "/api/debts", `{"name": "X", "balance": 0, "minimumPayment": 25}`, 400, "balance must be greater than zero"},
		{"create unknown field", http.MethodPost, "/api/debts", `{"name": "X", "colour": "red"}`, 400, "unknown field"},
		{"create bad json", http.MethodPost, "/api/debts", `{`, 400, ""},
		{"create bad money", http.MethodPost, "/api/debts", `{"name": "X", "balance": "lots"}`, 400, "money must be a number"},
		{"get bad id", http.MethodGet, "/api/debts/abc", nil, 404, "not found"},
		{"get missing", http.MethodGet, "/api/debts/999", nil, 404, "not found"},
		{"put bad id", http.MethodPut, "/api/debts/abc", `{}`, 404, "not found"},
		{"put missing", http.MethodPut, "/api/debts/999", `{"name": "X", "balance": 10, "minimumPayment": 5}`, 404, "not found"},
		{"put invalid", http.MethodPut, "/api/debts/999", `{"name": ""}`, 400, "debt name is required"},
		{"put bad json", http.MethodPut, "/api/debts/1", `nope`, 400, ""},
		{"delete bad id", http.MethodDelete, "/api/debts/abc", nil, 404, "not found"},
		{"delete missing", http.MethodDelete, "/api/debts/999", nil, 404, "not found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := e.sendJSON(tc.method, tc.path, tc.body)
			wantStatus(t, rec, tc.status)
			got := decodeJSON[map[string]string](t, rec)
			msg := got["error"]
			if msg == "" || !strings.Contains(msg, tc.want) {
				t.Fatalf("error = %q, want it to contain %q", msg, tc.want)
			}
			if strings.HasPrefix(msg, "invalid input") {
				t.Fatalf("error %q should not carry the internal prefix", msg)
			}
		})
	}
}

func TestAPISettings(t *testing.T) {
	e := newEnv(t)
	rec := e.get("/api/settings")
	wantStatus(t, rec, http.StatusOK)
	got := decodeJSON[map[string]any](t, rec)
	for _, k := range []string{"monthlyBudget", "extra", "strategy", "order", "startTotal"} {
		if _, ok := got[k]; !ok {
			t.Errorf("settings JSON missing %q: %v", k, got)
		}
	}
	if _, ok := got["planStart"]; ok {
		t.Errorf("planStart should be omitted before setup: %v", got)
	}

	rec = e.sendJSON(http.MethodPut, "/api/settings", `{"strategy": "snowball", "monthlyBudget": 500, "extra": 0, "order": {"by": "balance", "first": "low"}}`)
	wantStatus(t, rec, http.StatusOK)
	s := e.settings()
	if s.Strategy != "snowball" || s.MonthlyBudget != 50000 {
		t.Fatalf("saved settings = %+v", s)
	}
	rec = e.get("/api/settings")
	got = decodeJSON[map[string]any](t, rec)
	if got["strategy"] != "snowball" || got["monthlyBudget"] != 500.0 {
		t.Fatalf("settings after put = %v", got)
	}

	for name, body := range map[string]string{
		"bad strategy":    `{"strategy": "yolo"}`,
		"negative budget": `{"strategy": "avalanche", "monthlyBudget": -1}`,
		"unknown field":   `{"strategy": "avalanche", "nope": 1}`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := e.sendJSON(http.MethodPut, "/api/settings", body)
			wantStatus(t, rec, http.StatusBadRequest)
			if decodeJSON[map[string]string](t, rec)["error"] == "" {
				t.Fatal("missing error message")
			}
		})
	}
}

func TestAPIPreferences(t *testing.T) {
	e := newEnv(t)
	rec := e.get("/api/preferences")
	wantStatus(t, rec, http.StatusOK)
	got := decodeJSON[map[string]any](t, rec)
	if got["theme"] != "dark" || got["expandCurrent"] != true || got["noticeSeconds"] != 5.0 {
		t.Fatalf("default prefs = %v", got)
	}

	rec = e.sendJSON(http.MethodPut, "/api/preferences", `{"theme": "light", "expandCurrent": false, "noticeSeconds": 30}`)
	wantStatus(t, rec, http.StatusOK)
	got = decodeJSON[map[string]any](t, rec)
	if got["theme"] != "light" || got["expandCurrent"] != false || got["noticeSeconds"] != 30.0 {
		t.Fatalf("put prefs = %v", got)
	}
	if p := e.prefs(); p.Theme != "light" || p.ExpandCurrent || p.NoticeSeconds != 30 {
		t.Fatalf("stored prefs = %+v", p)
	}

	rec = e.sendJSON(http.MethodPut, "/api/preferences", `{"theme": "pink", "noticeSeconds": 5}`)
	wantStatus(t, rec, http.StatusBadRequest)
	if msg := decodeJSON[map[string]string](t, rec)["error"]; msg != "theme must be dark or light" {
		t.Fatalf("error = %q", msg)
	}
	rec = e.sendJSON(http.MethodPut, "/api/preferences", `{"theme": "dark", "noticeSeconds": 99}`)
	wantStatus(t, rec, http.StatusBadRequest)
	rec = e.sendJSON(http.MethodPut, "/api/preferences", `{"theme": "dark", "bogus": true}`)
	wantStatus(t, rec, http.StatusBadRequest)
	if p := e.prefs(); p.Theme != "light" {
		t.Fatalf("invalid put changed prefs: %+v", p)
	}
}

func TestAPIPlanAndCompare(t *testing.T) {
	e := newEnv(t)

	// No debts yet.
	rec := e.get("/api/plan")
	wantStatus(t, rec, http.StatusUnprocessableEntity)
	if msg := decodeJSON[map[string]string](t, rec)["error"]; msg != "no debts added yet" {
		t.Fatalf("error = %q", msg)
	}

	e.addDebt("Visa", 4500, 120, 24.99)
	e.addDebt("Store", 300, 35, 18)

	rec = e.get("/api/plan?strategy=snowball&budget=500&start=2026-01-01")
	wantStatus(t, rec, http.StatusOK)
	plan := decodeJSON[map[string]any](t, rec)
	if plan["strategy"] != "snowball" || plan["monthlyBudget"] != 500.0 || plan["months"].(float64) <= 0 {
		t.Fatalf("plan = strategy %v budget %v months %v", plan["strategy"], plan["monthlyBudget"], plan["months"])
	}
	if sched, ok := plan["schedule"].([]any); !ok || len(sched) == 0 {
		t.Fatalf("plan schedule missing")
	}
	if !strings.HasPrefix(plan["startDate"].(string), "2026-01-01") {
		t.Fatalf("startDate = %v", plan["startDate"])
	}

	rec = e.get("/api/compare?budget=500")
	wantStatus(t, rec, http.StatusOK)
	cmp := decodeJSON[[]map[string]any](t, rec)
	if len(cmp) == 0 {
		t.Fatal("compare returned nothing")
	}
	for _, p := range cmp {
		if p["schedule"] != nil {
			t.Fatalf("compare should omit schedules, got %v", p["schedule"])
		}
	}

	for _, q := range []string{"strategy=yolo", "budget=abc", "budget=-5", "start=tomorrow"} {
		t.Run(q, func(t *testing.T) {
			rec := e.get("/api/plan?" + q)
			wantStatus(t, rec, http.StatusBadRequest)
			if decodeJSON[map[string]string](t, rec)["error"] == "" {
				t.Fatal("missing error message")
			}
		})
	}
	wantStatus(t, e.get("/api/compare?strategy=yolo"), http.StatusBadRequest)

	// A budget below the minimums can't be planned.
	rec = e.get("/api/plan?budget=10")
	wantStatus(t, rec, http.StatusUnprocessableEntity)
}

func TestAPILumpSum(t *testing.T) {
	e := newEnv(t)
	rec := e.get("/api/lump-sum?amount=100")
	wantStatus(t, rec, http.StatusUnprocessableEntity)

	e.addDebt("Visa", 4500, 120, 24.99)
	e.addDebt("Store", 300, 35, 18)

	rec = e.get("/api/lump-sum?amount=1,000&budget=500")
	wantStatus(t, rec, http.StatusOK)
	a := decodeJSON[map[string]any](t, rec)
	if a["amount"] != 1000.0 {
		t.Fatalf("amount = %v", a["amount"])
	}
	opts, ok := a["options"].([]any)
	if !ok || len(opts) != 2 {
		t.Fatalf("want an option per debt, got %v", a["options"])
	}
	first := opts[0].(map[string]any)
	if first["recommended"] != true || first["target"] == "" {
		t.Fatalf("first option = %v", first)
	}

	for _, q := range []string{"", "amount=0", "amount=abc", "amount=-5", "amount=10&strategy=yolo"} {
		t.Run(q, func(t *testing.T) {
			rec := e.get("/api/lump-sum?" + q)
			wantStatus(t, rec, http.StatusBadRequest)
			if decodeJSON[map[string]string](t, rec)["error"] == "" {
				t.Fatal("missing error message")
			}
		})
	}
}

func TestLumpSumPageRedirect(t *testing.T) {
	e := newEnv(t)
	rec := e.get("/lump-sum?amount=500")
	wantStatus(t, rec, http.StatusMovedPermanently)
	if loc := rec.Header().Get("Location"); loc != "/simulate?amount=500&mode=one" {
		t.Fatalf("Location = %s", loc)
	}
	rec = e.get("/lump-sum")
	if loc := rec.Header().Get("Location"); loc != "/simulate?mode=one" {
		t.Fatalf("Location = %s", loc)
	}
}

func TestAPIUpdateDebtReturnsSavedDebt(t *testing.T) {
	e := newEnv(t)
	d := e.addDebt("Visa", 4500, 120, 24.99)

	rec := e.sendJSON(http.MethodPut, apiDebtPath(d.ID), `{"name": " Visa Gold ", "balance": 4000, "apr": 19.99, "minimumPayment": 110}`)
	wantStatus(t, rec, http.StatusOK)
	got := decodeJSON[apiDebt](t, rec)
	stored, err := e.debts.Get(e.ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != stored.Name || got.Type != stored.Type || !strings.HasPrefix(got.StartDate, stored.StartDate.Format("2006-01-02")) {
		t.Fatalf("PUT response %+v differs from the stored debt (name %q, type %q, start %s)",
			got, stored.Name, stored.Type, stored.StartDate.Format("2006-01-02"))
	}
}

// PUT /api/settings answers with what was saved: a planStart in the body is
// ignored (it's set by plan setup), so it isn't echoed back.
func TestAPISaveSettingsReturnsSaved(t *testing.T) {
	e := newEnv(t)
	rec := e.sendJSON(http.MethodPut, "/api/settings", `{"strategy": "avalanche", "monthlyBudget": 0, "extra": 25, "planStart": "2030-01-01T00:00:00Z", "order": {"by": "interest", "first": "high"}}`)
	wantStatus(t, rec, http.StatusOK)
	got := decodeJSON[map[string]any](t, rec)
	if _, ok := got["planStart"]; ok {
		t.Errorf("response echoes planStart it didn't save: %v", got)
	}
	if got["extra"] != 25.0 {
		t.Errorf("extra = %v, want 25", got["extra"])
	}
}
