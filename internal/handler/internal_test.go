package handler

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lee5i3/debt-reducer/internal/debt"
	"github.com/lee5i3/debt-reducer/internal/service"
)

func TestLocalURL(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/", "/"},
		{"/debts", "/debts"},
		{"/debts?q=visa", "/debts?q=visa"},
		{"/plan/months/2026-11/edit", "/plan/months/2026-11/edit"},
		{"", "/"},
		{"debts", "/"},
		{"//evil.com", "/"},
		{"//evil.com/x", "/"},
		{"https://evil.com/x", "/"},
		{"http://localhost:8080/debts", "/"},
		{"javascript:alert(1)", "/"},
		{`/\evil.com`, "/"},
		{"%zz", "/"},
		{"///evil.com", "/"},
	}
	for _, tc := range tests {
		if got := localURL(tc.in); got != tc.want {
			t.Errorf("localURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStatusFor(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{service.ErrNotFound, http.StatusNotFound},
		{fmt.Errorf("wrapped: %w", service.ErrNotFound), http.StatusNotFound},
		{service.ErrInvalid, http.StatusBadRequest},
		{fmt.Errorf("%w: name is required", service.ErrInvalid), http.StatusBadRequest},
		{debt.ErrInvalidAmount, http.StatusBadRequest},
		{debt.ErrBudgetTooLow, http.StatusUnprocessableEntity},
		{debt.ErrNeverPaidOff, http.StatusUnprocessableEntity},
		{debt.ErrNoDebts, http.StatusUnprocessableEntity},
		{errors.New("disk on fire"), http.StatusInternalServerError},
	}
	for _, tc := range tests {
		if got := statusFor(tc.err); got != tc.want {
			t.Errorf("statusFor(%v) = %d, want %d", tc.err, got, tc.want)
		}
	}
}

func TestUserMessage(t *testing.T) {
	h := &Handler{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	tests := []struct {
		err  error
		want string
	}{
		{fmt.Errorf("%w: name is required", service.ErrInvalid), "name is required"},
		{service.ErrNotFound, "not found"},
		{debt.ErrNoDebts, "no debts added yet"},
		{errors.New("sql: database is locked at /home/x/db"), "something went wrong"},
	}
	for _, tc := range tests {
		if got := h.userMessage(tc.err); got != tc.want {
			t.Errorf("userMessage(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestWriteErrorHidesInternals(t *testing.T) {
	h := &Handler{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	rec := httptest.NewRecorder()
	h.writeError(rec, errors.New("secret internal detail"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Body.String(); got != `{"error":"something went wrong"}`+"\n" {
		t.Fatalf("body = %q", got)
	}
}

func TestPlanOptions(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/plan?strategy=snowball&budget=1,234.50&start=2026-03-15", nil)
	opts, err := planOptions(r)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, time.March, 15, 0, 0, 0, 0, time.Local)
	if opts.Strategy != debt.Snowball || opts.MonthlyBudget != debt.FromDollars(1234.50) || !opts.StartDate.Equal(want) {
		t.Fatalf("opts = %+v", opts)
	}
	if opts, err := planOptions(httptest.NewRequest(http.MethodGet, "/api/plan", nil)); err != nil || opts != (service.Options{}) {
		t.Fatalf("empty query: opts=%+v err=%v", opts, err)
	}
	for _, q := range []string{"strategy=x", "budget=x", "budget=-1", "start=2026-13-01"} {
		if _, err := planOptions(httptest.NewRequest(http.MethodGet, "/api/plan?"+q, nil)); err == nil {
			t.Errorf("planOptions(%s): want error", q)
		}
	}
}

func TestDollarsText(t *testing.T) {
	for in, want := range map[debt.Money]string{0: "0.00", 123456: "1,234.56"} {
		if got := dollarsText(in); got != want {
			t.Errorf("dollarsText(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestReadSimInput(t *testing.T) {
	get := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

	in, err := readSimInput(get(nil))
	if err != nil || in.mode != "one" || in.amount != debt.FromDollars(5000) || in.month != 0 {
		t.Fatalf("defaults = %+v, %v", in, err)
	}
	in, err = readSimInput(get(map[string]string{"mode": "monthly", "month": "-3"}))
	if err != nil || in.mode != "monthly" || in.amount != debt.FromDollars(200) || in.month != 0 {
		t.Fatalf("monthly defaults = %+v, %v", in, err)
	}
	in, _ = readSimInput(get(map[string]string{"month": "50"}))
	if in.month != simMonths-1 {
		t.Fatalf("month clamp = %d", in.month)
	}
	if _, err := readSimInput(get(map[string]string{"amount": "0"})); err == nil {
		t.Fatal("zero amount: want error")
	}

	one := simInput{mode: "one", amount: 100, month: 2}
	monthly := simInput{mode: "monthly", amount: 100, month: 2}
	for i, want := range []debt.Money{0, 0, 100, 0} {
		if got := one.extra(i); got != want {
			t.Errorf("one.extra(%d) = %d, want %d", i, got, want)
		}
	}
	for i, want := range []debt.Money{0, 0, 100, 100} {
		if got := monthly.extra(i); got != want {
			t.Errorf("monthly.extra(%d) = %d, want %d", i, got, want)
		}
	}
}

func TestComesFirst(t *testing.T) {
	hiAPR := debt.Debt{Name: "A", APR: 25, Balance: 1000}
	loAPR := debt.Debt{Name: "B", APR: 10, Balance: 200}
	tests := []struct {
		name string
		s    debt.Strategy
		o    debt.Order
		want bool // hiAPR before loAPR
	}{
		{"avalanche", debt.Avalanche, debt.Order{}, true},
		{"snowball", debt.Snowball, debt.Order{}, false},
		{"custom interest high", debt.Custom, debt.Order{By: debt.OrderByInterest, First: debt.OrderHighFirst}, true},
		{"custom interest low", debt.Custom, debt.Order{By: debt.OrderByInterest, First: debt.OrderLowFirst}, false},
		{"custom balance high", debt.Custom, debt.Order{By: debt.OrderByBalance, First: debt.OrderHighFirst}, true},
		{"custom balance low", debt.Custom, debt.Order{By: debt.OrderByBalance, First: debt.OrderLowFirst}, false},
	}
	for _, tc := range tests {
		if got := comesFirst(hiAPR, loAPR, tc.s, tc.o); got != tc.want {
			t.Errorf("%s: comesFirst = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestVisibleMonths(t *testing.T) {
	months := make([]service.PlanMonth, 20)
	months[0].Status, months[1].Status = debt.MonthPaid, debt.MonthSkipped
	rows, hidden := visibleMonths(months, false)
	if len(rows) != 2+rowsShown || hidden != 20-2-rowsShown {
		t.Fatalf("rows=%d hidden=%d", len(rows), hidden)
	}
	rows, hidden = visibleMonths(months, true)
	if len(rows) != 20 || hidden != 0 {
		t.Fatalf("all: rows=%d hidden=%d", len(rows), hidden)
	}
	rows, hidden = visibleMonths(months[:5], false)
	if len(rows) != 5 || hidden != 0 {
		t.Fatalf("short: rows=%d hidden=%d", len(rows), hidden)
	}
}

func TestMonthOptions(t *testing.T) {
	now := service.FirstOfMonth(time.Now())
	opts := monthOptions(now)
	if len(opts) != 10 || opts[0].Value != now.AddDate(0, -6, 0).Format("2006-01") || opts[9].Value != now.AddDate(0, 3, 0).Format("2006-01") {
		t.Fatalf("options = %+v", opts)
	}
	old := now.AddDate(-2, 0, 0)
	opts = monthOptions(old)
	if opts[0].Value != old.Format("2006-01") || len(opts) != 24+3+1 {
		t.Fatalf("old start: first=%s len=%d", opts[0].Value, len(opts))
	}
}

func TestFilterDebts(t *testing.T) {
	debts := []debt.Debt{{Name: "Visa", Type: debt.TypeCard}, {Name: "Car", Type: debt.TypeLoan}}
	if got := filterDebts(debts, ""); len(got) != 2 {
		t.Fatalf("empty query: %d", len(got))
	}
	if got := filterDebts(debts, "LOAN"); len(got) != 1 || got[0].Name != "Car" {
		t.Fatalf("type match: %+v", got)
	}
	if got := filterDebts(debts, "vis"); len(got) != 1 || got[0].Name != "Visa" {
		t.Fatalf("name match: %+v", got)
	}
}
