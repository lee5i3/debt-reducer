package handler_test

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/lee5i3/debt-reducer/internal/debt"
)

func validDebtForm() url.Values {
	return url.Values{
		"name":            {"Visa"},
		"type":            {debt.TypeCard},
		"balance":         {"4,500.00"},
		"apr":             {"24.99"},
		"minimum_payment": {"120.00"},
		"due_day":         {"15"},
		"start_date":      {today().Format(time.DateOnly)},
		"active":          {"1"},
	}
}

func debtPath(id int64, suffix string) string {
	return "/debts/" + strconv.FormatInt(id, 10) + suffix
}

func TestDebtsPageEmptyAndList(t *testing.T) {
	e := newEnv(t)
	rec := e.get("/debts")
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "<h1>Debts</h1>", "No debts yet.")

	e.addDebt("Visa", 4500, 120, 24.99)
	e.addDebt("Store", 300, 35, 18)
	rec = e.get("/debts")
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "Visa", "Store", "2 debts · $4,800.00 owed · $155.00/mo in minimums")
	wantNoBody(t, rec, "No debts yet.", "inactive, not counted")
}

func TestDebtsPageSearch(t *testing.T) {
	e := newEnv(t)
	e.addDebt("Visa", 4500, 120, 24.99)
	e.addDebt("Store", 300, 35, 18)
	rec := e.get("/debts?q=vis")
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "1 of 2 debts match", "Clear search")
	wantNoBody(t, rec, "View details for Store")

	rec = e.get("/debts?q=nothing")
	wantBody(t, rec, "No debts match your search.")
}

func TestDebtsPageTotalsExcludeInactive(t *testing.T) {
	e := newEnv(t)
	e.addDebt("Visa", 4500, 120, 24.99)
	store := e.addDebt("Store", 300, 35, 18)
	if err := e.debts.SetActive(e.ctx, store.ID, false); err != nil {
		t.Fatal(err)
	}
	rec := e.get("/debts")
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "2 debts · $4,500.00 owed · $120.00/mo in minimums · 1 inactive, not counted", "Inactive")
}

func TestCreateDebtValid(t *testing.T) {
	e := newEnv(t)
	u := wantRedirect(t, e.postForm("/debts", validDebtForm()))
	if u.Path != "/debts" || u.Query().Get("msg") != "added" || u.Query().Get("name") != "Visa" {
		t.Fatalf("Location = %s, want /debts?msg=added&name=Visa", u)
	}
	list, _ := e.debts.List(e.ctx)
	if len(list) != 1 {
		t.Fatalf("want 1 debt, got %d", len(list))
	}
	d := list[0]
	if d.Name != "Visa" || d.Balance != debt.FromDollars(4500) || d.MinimumPayment != debt.FromDollars(120) ||
		d.APR != 24.99 || d.DueDay != 15 || d.Inactive {
		t.Fatalf("stored debt = %+v", d)
	}

	// The redirect target shows the notice banner.
	rec := e.get(u.String())
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "Visa added. Your plan has been updated.", `role="status" data-autoclose`)
}

func TestCreateDebtActiveCheckbox(t *testing.T) {
	e := newEnv(t)
	form := validDebtForm()
	form.Del("active")
	form.Set("name", "Off")
	wantRedirect(t, e.postForm("/debts", form))
	form = validDebtForm()
	form.Set("name", "On")
	wantRedirect(t, e.postForm("/debts", form))

	list, _ := e.debts.List(e.ctx)
	got := map[string]bool{}
	for _, d := range list {
		got[d.Name] = d.Inactive
	}
	if inactive, ok := got["Off"]; !ok || !inactive {
		t.Errorf("debt without active checkbox: inactive=%v (found %v), want inactive", inactive, ok)
	}
	if inactive, ok := got["On"]; !ok || inactive {
		t.Errorf("debt with active=1: inactive=%v (found %v), want active", inactive, ok)
	}
}

func TestCreateDebtCalculatesMinimum(t *testing.T) {
	e := newEnv(t)
	form := validDebtForm()
	form.Del("minimum_payment")
	form.Set("balance", "1000")
	form.Set("apr", "0")
	wantRedirect(t, e.postForm("/debts", form))
	list, _ := e.debts.List(e.ctx)
	if len(list) != 1 || list[0].MinimumPayment != debt.FromDollars(25) {
		t.Fatalf("want calculated $25 minimum (1%% of $1,000 floored at $25), got %+v", list)
	}
}

func TestCreateDebtInvalid(t *testing.T) {
	tomorrow := today().AddDate(0, 0, 1).Format(time.DateOnly)
	tests := []struct {
		name string
		edit func(url.Values)
		want string
	}{
		{"missing name", func(v url.Values) { v.Set("name", "  ") }, "Give this debt a name."},
		{"future start date", func(v url.Values) { v.Set("start_date", tomorrow) }, "Pick today or an earlier date."},
		{"bad start date", func(v url.Values) { v.Set("start_date", "nope") }, "Pick the date this balance is from."},
		{"bad balance", func(v url.Values) { v.Set("balance", "abc") }, "Enter the balance as a number above 0."},
		{"negative apr", func(v url.Values) { v.Set("apr", "-1") }, "Enter a number, 0 or higher."},
		{"bad due day", func(v url.Values) { v.Set("due_day", "40") }, "Pick a day from 1 to 31, or Not set."},
		{"bad type", func(v url.Values) { v.Set("type", "Mortgage") }, "Pick a type from the list."},
		{"bad payment", func(v url.Values) { v.Set("minimum_payment", "0") }, "Enter the payment as a number above 0."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			form := validDebtForm()
			tc.edit(form)
			rec := e.postForm("/debts", form)
			wantStatus(t, rec, http.StatusBadRequest)
			wantBody(t, rec, tc.want, "<dialog")
			if list, _ := e.debts.List(e.ctx); len(list) != 0 {
				t.Fatalf("invalid form saved a debt: %+v", list)
			}
		})
	}
}

func TestCreateDebtCalculateButton(t *testing.T) {
	e := newEnv(t)
	form := url.Values{"name": {"Car"}, "type": {debt.TypeLoan}, "balance": {"12000"}, "apr": {"0"},
		"term_months": {"12"}, "calculate": {"1"}}
	rec := e.postForm("/debts", form)
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "1,000.00") // 12,000 over 12 months at 0%
	if list, _ := e.debts.List(e.ctx); len(list) != 0 {
		t.Fatal("calculate must not save")
	}

	form.Del("term_months")
	rec = e.postForm("/debts", form)
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "Enter months remaining to calculate the payment.")

	form = url.Values{"name": {"Promo"}, "type": {debt.TypePromo}, "add_purchase": {"1"},
		"promo_remaining": {"100"}, "promo_ends": {today().AddDate(1, 0, 0).Format(time.DateOnly)}}
	rec = e.postForm("/debts", form)
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, `name="promo_remaining"`)
}

func TestCreatePromoDebt(t *testing.T) {
	e := newEnv(t)
	ends := today().AddDate(1, 0, 0).Format(time.DateOnly)
	form := url.Values{"name": {"Furniture"}, "type": {debt.TypePromo}, "due_day": {"1"}, "active": {"1"},
		"promo_remaining": {"1,200.00", ""}, "promo_ends": {ends, ""}}
	wantRedirect(t, e.postForm("/debts", form))
	list, _ := e.debts.List(e.ctx)
	if len(list) != 1 || list[0].Balance != debt.FromDollars(1200) || len(list[0].Promos) != 1 {
		t.Fatalf("promo debt = %+v", list)
	}

	// An expired promotion is rejected.
	form.Set("promo_ends", today().Format(time.DateOnly))
	rec := e.postForm("/debts", form)
	wantStatus(t, rec, http.StatusBadRequest)
	wantBody(t, rec, "This promotion has expired. Pick a date after today.")
}

func TestUpdateDebt(t *testing.T) {
	e := newEnv(t)
	d := e.addDebt("Visa", 4500, 120, 24.99)
	form := validDebtForm()
	form.Set("name", "Visa Gold")
	form.Set("minimum_payment", "150")
	u := wantRedirect(t, e.postForm(debtPath(d.ID, ""), form))
	if u.Query().Get("msg") != "updated" || u.Query().Get("name") != "Visa Gold" {
		t.Fatalf("Location = %s", u)
	}
	got, err := e.debts.Get(e.ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Visa Gold" || got.MinimumPayment != debt.FromDollars(150) {
		t.Fatalf("updated debt = %+v", got)
	}

	// Invalid edits re-show the form.
	form.Set("name", "")
	rec := e.postForm(debtPath(d.ID, ""), form)
	wantStatus(t, rec, http.StatusBadRequest)
	wantBody(t, rec, "Give this debt a name.")

	// Unknown and malformed ids.
	rec = e.postForm(debtPath(9999, ""), validDebtForm())
	wantStatus(t, rec, http.StatusNotFound)
	rec = e.postForm("/debts/abc", validDebtForm())
	wantStatus(t, rec, http.StatusNotFound)
}

func TestSetDebtActive(t *testing.T) {
	e := newEnv(t)
	d := e.addDebt("Visa", 4500, 120, 24.99)

	u := wantRedirect(t, e.postForm(debtPath(d.ID, "/active"), url.Values{}))
	if u.Query().Get("msg") != "deactivated" || u.Query().Get("name") != "Visa" {
		t.Fatalf("Location = %s", u)
	}
	if got, _ := e.debts.Get(e.ctx, d.ID); !got.Inactive {
		t.Fatal("debt should be inactive")
	}
	rec := e.get(u.String())
	wantBody(t, rec, "Visa is now left out of your plan and Simulate.")

	u = wantRedirect(t, e.postForm(debtPath(d.ID, "/active"), url.Values{"active": {"1"}}))
	if u.Query().Get("msg") != "activated" {
		t.Fatalf("Location = %s", u)
	}
	if got, _ := e.debts.Get(e.ctx, d.ID); got.Inactive {
		t.Fatal("debt should be active")
	}

	wantStatus(t, e.postForm(debtPath(9999, "/active"), url.Values{"active": {"1"}}), http.StatusNotFound)
	wantStatus(t, e.postForm("/debts/abc/active", url.Values{}), http.StatusNotFound)
}

func TestDeleteDebt(t *testing.T) {
	e := newEnv(t)
	d := e.addDebt("Visa", 4500, 120, 24.99)

	rec := e.get(debtPath(d.ID, "/delete"))
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "Visa", `action="/debts/`+strconv.FormatInt(d.ID, 10)+`/delete"`)

	u := wantRedirect(t, e.postForm(debtPath(d.ID, "/delete"), nil))
	if u.Query().Get("msg") != "deleted" || u.Query().Get("name") != "Visa" {
		t.Fatalf("Location = %s", u)
	}
	if list, _ := e.debts.List(e.ctx); len(list) != 0 {
		t.Fatal("debt not deleted")
	}
	wantBody(t, e.get(u.String()), "Visa deleted.")

	// Deleting again goes back to the list without a notice.
	u = wantRedirect(t, e.postForm(debtPath(d.ID, "/delete"), nil))
	if u.String() != "/debts" {
		t.Fatalf("Location = %s, want /debts", u)
	}
	wantStatus(t, e.postForm("/debts/abc/delete", nil), http.StatusNotFound)
}

func TestDebtDetailPage(t *testing.T) {
	e := newEnv(t)
	d := e.addDebt("Visa", 4500, 120, 24.99)
	e.addDebt("Store", 300, 35, 18)

	rec := e.get(debtPath(d.ID, ""))
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, `id="debt-detail-title"`, "Visa", "Left", "Started at $4,500.00", "Where the minimum goes", "$120.00/mo")
	// 4500 of 4800 is 94%.
	wantBody(t, rec, "<b>94%</b>")

	// With a plan set up the detail still renders.
	if err := e.plans.Setup(e.ctx, debt.Avalanche, debt.Order{}, 0, debt.FromDollars(200), thisMonth()); err != nil {
		t.Fatal(err)
	}
	rec = e.get(debtPath(d.ID, ""))
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "Started at $4,500.00")
}

func TestDebtDialogPages(t *testing.T) {
	e := newEnv(t)
	d := e.addDebt("Visa", 4500, 120, 24.99)

	rec := e.get("/debts/new")
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "<dialog", `action="/debts"`)

	rec = e.get(debtPath(d.ID, "/edit"))
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "<dialog", `value="Visa"`, `action="/debts/`+strconv.FormatInt(d.ID, 10)+`"`)
}

func TestDebtUnknownIDPages(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/debts/9999", "/debts/9999/edit", "/debts/9999/delete", "/debts/abc", "/debts/abc/edit"} {
		t.Run(p, func(t *testing.T) {
			rec := e.get(p)
			wantStatus(t, rec, http.StatusNotFound)
			wantNoBody(t, rec, "sql:", "no rows")
		})
	}
}

func TestMinimumPaymentAPI(t *testing.T) {
	e := newEnv(t)

	rec := e.get("/api/minimum-payment?type=" + url.QueryEscape(debt.TypeCard) + "&balance=1000&apr=0")
	wantStatus(t, rec, http.StatusOK)
	got := decodeJSON[map[string]string](t, rec)
	if got["amount"] != "$25.00/mo" || got["value"] != "25.00" || got["rule"] == "" {
		t.Fatalf("card minimum = %v", got)
	}

	rec = e.get("/api/minimum-payment?type=" + url.QueryEscape(debt.TypeLoan) + "&balance=12000&apr=0&term_months=12")
	got = decodeJSON[map[string]string](t, rec)
	if got["amount"] != "$1,000.00/mo" || got["value"] != "1,000.00" {
		t.Fatalf("loan minimum = %v", got)
	}

	rec = e.get("/api/minimum-payment?type=" + url.QueryEscape(debt.TypeLoan) + "&balance=12000&apr=0")
	got = decodeJSON[map[string]string](t, rec)
	if got["amount"] != "—" || got["value"] != "" || got["rule"] != "Enter months remaining to calculate the payment." {
		t.Fatalf("loan without months = %v", got)
	}

	rec = e.get("/api/minimum-payment?balance=abc")
	got = decodeJSON[map[string]string](t, rec)
	if got["amount"] != "—" || got["rule"] != "Enter balance and APR to calculate." {
		t.Fatalf("invalid input = %v", got)
	}
}
