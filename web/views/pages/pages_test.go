package pages

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"

	"github.com/lee5i3/debt-reducer/internal/debt"
	"github.com/lee5i3/debt-reducer/internal/service"
	"github.com/lee5i3/debt-reducer/web/views/components"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

var tagRE = regexp.MustCompile(`<[a-zA-Z][^>]*>`)

// tag returns the first opening tag that contains all the given fragments.
func tag(html string, fragments ...string) string {
	for _, m := range tagRE.FindAllString(html, -1) {
		ok := true
		for _, f := range fragments {
			if !strings.Contains(m, f) {
				ok = false
				break
			}
		}
		if ok {
			return m
		}
	}
	return ""
}

// text strips tags and collapses whitespace, for checking visible text.
func text(html string) string {
	return strings.Join(strings.Fields(tagRE.ReplaceAllString(regexp.MustCompile(`</[^>]+>`).ReplaceAllString(html, " "), " ")), " ")
}

func d(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 0, 0, 0, 0, time.Local) }

// ---- format.go helpers ----

func TestStartedText(t *testing.T) {
	if got := startedText(debt.Debt{Balance: 450000, StartDate: d(2026, time.November, 21)}); got != "Started at $4,500.00 · Nov 21, 2026" {
		t.Errorf("got %q", got)
	}
	if got := startedText(debt.Debt{Balance: 12345}); got != "Started at $123.45" {
		t.Errorf("no date: got %q", got)
	}
}

func TestDebtsSummary(t *testing.T) {
	base := DebtsData{TotalCount: 3, TotalBalance: 1234567, MinimumTotal: 40500}
	if got := debtsSummary(base); got != "3 debts · $12,345.67 owed · $405.00/mo in minimums" {
		t.Errorf("got %q", got)
	}
	base.Inactive = 2
	if got := debtsSummary(base); got != "3 debts · $12,345.67 owed · $405.00/mo in minimums · 2 inactive, not counted" {
		t.Errorf("with inactive: got %q", got)
	}
}

func TestPlanPayoffText(t *testing.T) {
	jul := d(2028, time.July, 1)
	cases := []struct {
		name string
		x    DebtDetail
		want string
	}{
		{"paid off", DebtDetail{Debt: debt.Debt{Balance: 0, Inactive: true}, PlanPayoff: jul, PlanPayments: 3}, "Paid off"},
		{"inactive", DebtDetail{Debt: debt.Debt{Balance: 100, Inactive: true}, PlanPayoff: jul, PlanPayments: 3}, "Inactive, not in your plan"},
		{"not in plan", DebtDetail{Debt: debt.Debt{Balance: 100}}, "Not within your plan"},
		{"next payment", DebtDetail{Debt: debt.Debt{Balance: 100}, PlanPayoff: jul, PlanPayments: 1}, "Jul 2028 · next payment"},
		{"many", DebtDetail{Debt: debt.Debt{Balance: 100}, PlanPayoff: jul, PlanPayments: 20}, "Jul 2028 · 20 payments"},
	}
	for _, c := range cases {
		if got := planPayoffText(c.x); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDebtDetailMinimumSplit(t *testing.T) {
	x := DebtDetail{Debt: debt.Debt{MinimumPayment: 10000}, InterestThisMonth: 2500}
	if x.MinimumInterest() != 2500 || x.OffBalance() != 7500 || x.InterestPercent() != 25 {
		t.Errorf("split = %s / %s / %d%%", x.MinimumInterest(), x.OffBalance(), x.InterestPercent())
	}
	// Interest bigger than the minimum: all of it goes to interest.
	x.InterestThisMonth = 20000
	if x.MinimumInterest() != 10000 || x.OffBalance() != 0 || x.InterestPercent() != 100 {
		t.Errorf("capped split = %s / %s / %d%%", x.MinimumInterest(), x.OffBalance(), x.InterestPercent())
	}
	if (DebtDetail{}).InterestPercent() != 0 {
		t.Error("no minimum should be 0%")
	}
}

func TestAprText(t *testing.T) {
	now := time.Now()
	if got := aprText(debt.Debt{Type: debt.TypeCard, APR: 24.99}); got != "24.99%" {
		t.Errorf("card: %q", got)
	}
	ends := now.AddDate(0, 4, 3)
	if got, want := aprText(debt.Debt{Type: debt.TypePromo, APR: 26.99, Promos: []debt.PromoItem{{Remaining: 100, Ends: ends}}}), "0% until "+ends.Format("Jan 2006"); got != want {
		t.Errorf("promo purchases: %q, want %q", got, want)
	}
	term := now.AddDate(0, 6, 0).Add(time.Hour)
	if got, want := aprText(debt.Debt{Type: debt.TypePromo, APR: 26.99, TermEnds: &term}), "0% for "+itoa(debt.MonthsUntil(&term, now))+" mo"; got != want {
		t.Errorf("promo term: %q, want %q", got, want)
	}
	past := now.AddDate(0, -1, 0)
	if got := aprText(debt.Debt{Type: debt.TypePromo, APR: 26.99, TermEnds: &past}); got != "26.99%" {
		t.Errorf("expired promo: %q", got)
	}
}

func TestPromoTip(t *testing.T) {
	x := debt.Debt{Promos: []debt.PromoItem{
		{Remaining: 140938, Ends: d(2029, time.April, 13)},
		{Remaining: 0, Ends: d(2029, time.May, 1)},
		{Remaining: 42712, Ends: d(2029, time.June, 13)},
	}}
	got := promoTip(x)
	if len(got) != 2 || got[0] != "Apr 13, 2029: $1,409.38 left" || got[1] != "Jun 13, 2029: $427.12 left" {
		t.Errorf("got %q", got)
	}
	term := d(2027, time.March, 5)
	if got := promoTip(debt.Debt{TermEnds: &term}); len(got) != 1 || got[0] != "Mar 5, 2027" {
		t.Errorf("term only: %q", got)
	}
	// All purchases paid falls back to the term.
	if got := promoTip(debt.Debt{TermEnds: &term, Promos: []debt.PromoItem{{Remaining: 0}}}); len(got) != 1 || got[0] != "Mar 5, 2027" {
		t.Errorf("paid purchases: %q", got)
	}
	if got := promoTip(debt.Debt{}); got != nil {
		t.Errorf("nothing: %q", got)
	}
}

func TestSmallFormatters(t *testing.T) {
	if moneyText(123456) != "1,234.56" || moneyText(-500) != "-$5.00" && moneyText(-500) != "-5.00" {
		t.Errorf("moneyText = %q / %q", moneyText(123456), moneyText(-500))
	}
	if moneyText(0) != "0.00" {
		t.Errorf("moneyText(0) = %q", moneyText(0))
	}
	ord := map[int]string{1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 11: "11th", 12: "12th", 13: "13th", 21: "21st", 22: "22nd", 23: "23rd", 31: "31st", 101: "101st", 111: "111th"}
	for n, want := range ord {
		if got := ordinal(n); got != want {
			t.Errorf("ordinal(%d) = %q, want %q", n, got, want)
		}
	}
	dur := map[int]string{0: "0 mo", 5: "5 mo", 12: "1 yr", 24: "2 yr", 14: "1 yr 2 mo"}
	for n, want := range dur {
		if got := duration(n); got != want {
			t.Errorf("duration(%d) = %q, want %q", n, got, want)
		}
	}
	tm := d(2026, time.March, 5)
	if date(tm) != "Mar 5, 2026" || monthYear(tm) != "March 2026" || isoDate(tm) != "2026-03-05" {
		t.Errorf("dates: %q %q %q", date(tm), monthYear(tm), isoDate(tm))
	}
	if dollars(123456) != "1234.56" || percent(6.5) != "6.50%" || itoa(42) != "42" {
		t.Errorf("dollars/percent/itoa: %q %q %q", dollars(123456), percent(6.5), itoa(42))
	}
	if ariaInvalid("x") != "true" || ariaInvalid("") != "false" {
		t.Error("ariaInvalid")
	}
	if string(debtURL(7, "/edit")) != "/debts/7/edit" || string(debtURL(7, "")) != "/debts/7" {
		t.Error("debtURL")
	}
	if string(formAction(DebtForm{})) != "/debts" || string(formAction(DebtForm{ID: 4})) != "/debts/4" {
		t.Error("formAction")
	}
	if namePlaceholder(debt.TypeLoan) != "e.g. Car loan" || namePlaceholder(debt.TypePromo) != "e.g. Furniture financing" || namePlaceholder("") != "e.g. Visa card" {
		t.Error("namePlaceholder")
	}
	if len(dueDays) != 31 || dueDays[0] != "1" || dueDays[30] != "31" {
		t.Errorf("dueDays = %v", dueDays)
	}
	if plural(1, "month", "months") != "month" || plural(2, "month", "months") != "months" || plural(0, "month", "months") != "months" {
		t.Error("plural")
	}
	if firstNonEmpty("a", "b") != "a" || firstNonEmpty("", "b") != "b" {
		t.Error("firstNonEmpty")
	}
	if _, err := time.Parse(time.DateOnly, tomorrow()); err != nil {
		t.Errorf("tomorrow = %q", tomorrow())
	}
	if todayISO() != time.Now().Format(time.DateOnly) {
		t.Errorf("todayISO = %q", todayISO())
	}
}

func TestDebtFormHelpers(t *testing.T) {
	f := DebtForm{Errors: map[string]string{"name": "required"}}
	if f.Editing() || f.Err("name") != "required" || f.Err("apr") != "" {
		t.Error("Editing/Err")
	}
	if (DebtForm{}).Err("x") != "" {
		t.Error("nil errors map should give empty")
	}
}

func TestFormFromDebt(t *testing.T) {
	today := d(2026, time.November, 21)

	card := debt.Debt{ID: 3, Name: "Visa", Type: debt.TypeCard, Balance: 450000, APR: 24.99, MinimumPayment: 12000, DueDay: 15,
		StartDate: d(2026, time.October, 2)}
	f := FormFromDebt(card, today)
	if f.ID != 3 || !f.Editing() || f.Name != "Visa" || f.Type != debt.TypeCard {
		t.Errorf("identity = %+v", f)
	}
	if !f.Active {
		t.Error("an active debt should give Active = true")
	}
	if f.Balance != "4,500.00" || f.APR != "24.99" || f.MinimumPayment != "120.00" || f.DueDay != "15" || f.StartDate != "2026-10-02" {
		t.Errorf("fields = %+v", f)
	}
	if len(f.Purchases) != 1 || f.Purchases[0] != (PurchaseRow{}) || f.PurchaseTotal != "" || f.TermMonths != "" {
		t.Errorf("card extras = %+v", f)
	}

	// Inactive, no due day, no start date.
	f = FormFromDebt(debt.Debt{Name: "Old", Inactive: true, Balance: 100, MinimumPayment: 100, APR: 6.5}, today)
	if f.Active || f.DueDay != "" || f.StartDate != "" || f.APR != "6.50" {
		t.Errorf("inactive form = %+v", f)
	}

	// Loan: months left from the term.
	ends := d(2028, time.March, 1)
	f = FormFromDebt(debt.Debt{Type: debt.TypeLoan, Balance: 800000, MinimumPayment: 25000, TermEnds: &ends}, today)
	if f.TermMonths != "16" {
		t.Errorf("loan months = %q, want 16", f.TermMonths)
	}
	past := d(2025, time.January, 1)
	if f = FormFromDebt(debt.Debt{Type: debt.TypeLoan, Balance: 1, MinimumPayment: 1, TermEnds: &past}, today); f.TermMonths != "" {
		t.Errorf("ended loan months = %q", f.TermMonths)
	}

	// Promo with purchases.
	promo := debt.Debt{Type: debt.TypePromo, Balance: 183650, MinimumPayment: 6238, Promos: []debt.PromoItem{
		{Remaining: 140938, Ends: d(2029, time.April, 13)},
		{Remaining: 42712, Ends: d(2029, time.June, 13)},
	}}
	f = FormFromDebt(promo, today)
	if len(f.Purchases) != 2 || f.Purchases[0] != (PurchaseRow{Remaining: "1,409.38", Ends: "2029-04-13"}) || f.Purchases[1].Remaining != "427.12" {
		t.Errorf("purchases = %+v", f.Purchases)
	}
	if f.PurchaseTotal != "$1,836.50" {
		t.Errorf("purchase total = %q", f.PurchaseTotal)
	}

	// Older promo saved with only a term.
	term := d(2027, time.June, 30)
	f = FormFromDebt(debt.Debt{Type: debt.TypePromo, Balance: 50000, MinimumPayment: 2500, TermEnds: &term}, today)
	if len(f.Purchases) != 1 || f.Purchases[0] != (PurchaseRow{Remaining: "500.00", Ends: "2027-06-30"}) {
		t.Errorf("legacy promo = %+v", f.Purchases)
	}
}

// ---- plan.go helpers ----

func TestMonthParamAndURLs(t *testing.T) {
	m := d(2027, time.March, 1)
	if MonthParam(m) != "2027-03" {
		t.Errorf("MonthParam = %q", MonthParam(m))
	}
	if string(planMonthURL(m, "paid")) != "/plan/months/2027-03/paid" {
		t.Errorf("planMonthURL = %q", planMonthURL(m, "paid"))
	}
	if monthShort(m) != "Mar 2027" || monthLong(m) != "March 2027" {
		t.Error("month formats")
	}
	if string(simTabURL("one")) != "/simulate?mode=one" {
		t.Error("simTabURL")
	}
	got := string(simAllURL(SimData{Mode: "one", Amount: "1,000", Month: "2"}))
	if got != "/simulate?all=1&amount=1%2C000&mode=one&month=2" {
		t.Errorf("simAllURL = %q", got)
	}
	if string(widthPercent(42)) != "width:42%" {
		t.Errorf("widthPercent = %q", widthPercent(42))
	}
	if today().Day() != 1 {
		t.Error("today() should be the first of the month")
	}
}

func TestStrategyTextAndTypeName(t *testing.T) {
	cases := map[debt.Strategy]string{
		debt.Avalanche:   "Avalanche (highest interest first)",
		debt.Snowball:    "Snowball (smallest balance first)",
		debt.MinimumOnly: "Minimum (payments only)",
		"":               "Avalanche (highest interest first)",
	}
	for s, want := range cases {
		if got := strategyText(s, debt.Order{}); got != want {
			t.Errorf("strategyText(%q) = %q, want %q", s, got, want)
		}
	}
	if got := strategyText(debt.Custom, debt.Order{By: debt.OrderByBalance, First: debt.OrderLowFirst}); got != "Custom (lowest balance first)" {
		t.Errorf("custom = %q", got)
	}
	if typeName(debt.MinimumOnly) != "Minimum" || typeName(debt.Snowball) != "Snowball" || typeName(debt.Custom) != "Custom" {
		t.Error("typeName")
	}
}

func TestStatusTextAndClass(t *testing.T) {
	cases := []struct {
		name        string
		m           service.PlanMonth
		text, class string
	}{
		{"paid", service.PlanMonth{Status: debt.MonthPaid, Next: true, Edited: true}, "Paid", "is-paid"},
		{"skipped", service.PlanMonth{Status: debt.MonthSkipped, Edited: true}, "Skipped", "is-skipped"},
		{"next", service.PlanMonth{Next: true, Edited: true}, "Current", "is-next"},
		{"edited", service.PlanMonth{Edited: true}, "Edited", "is-upcoming"},
		{"upcoming", service.PlanMonth{}, "Upcoming", "is-upcoming"},
	}
	for _, c := range cases {
		if got := statusText(c.m); got != c.text {
			t.Errorf("%s: statusText = %q, want %q", c.name, got, c.text)
		}
		if got := statusClass(c.m); got != c.class {
			t.Errorf("%s: statusClass = %q, want %q", c.name, got, c.class)
		}
	}
}

func TestRoleClass(t *testing.T) {
	cases := map[string]string{
		debt.RolePaidOff: "is-paid",
		debt.RoleExtra:   "is-extra",
		debt.RoleSkipped: "is-skipped",
		debt.RoleMinimum: "is-min",
		"":               "is-min",
	}
	for role, want := range cases {
		if got := roleClass(role); got != want {
			t.Errorf("roleClass(%q) = %q, want %q", role, got, want)
		}
	}
}

func TestMoneyOrDash(t *testing.T) {
	if moneyOrDash(12345, false) != "$123.45" || moneyOrDash(12345, true) != "—" {
		t.Error("moneyOrDash")
	}
}

func TestProgressPercent(t *testing.T) {
	cases := []struct {
		start, owed debt.Money
		want        int
	}{
		{0, 0, 0},
		{-5, 0, 0},
		{100000, 100000, 0},
		{100000, 75000, 25},
		{100000, 0, 100},
		{100000, 33333, 66}, // truncates
		{100000, 150000, 0}, // owes more than at the start: clamped
		{100000, -100, 100}, // clamped at 100
	}
	for _, c := range cases {
		if got := progressPercent(&service.TrackedPlan{StartTotal: c.start, Owed: c.owed}); got != c.want {
			t.Errorf("progress(%d, %d) = %d, want %d", c.start, c.owed, got, c.want)
		}
	}
}

func TestMinimumPartAndBaseRole(t *testing.T) {
	cases := []struct {
		name    string
		x       debt.MonthDebt
		part    debt.Money
		role    string
		hasLine bool
	}{
		{"minimum only", debt.MonthDebt{Paid: 12000, Minimum: 12000, Role: debt.RoleMinimum}, 12000, debt.RoleMinimum, false},
		{"with extra", debt.MonthDebt{Paid: 30000, Minimum: 12000, Role: debt.RoleExtra}, 12000, debt.RoleMinimum, true},
		{"extra role but no extra", debt.MonthDebt{Paid: 12000, Minimum: 12000, Role: debt.RoleExtra}, 12000, debt.RoleExtra, false},
		{"paid off with extra", debt.MonthDebt{Paid: 30000, Minimum: 12000, Role: debt.RolePaidOff}, 12000, debt.RolePaidOff, true},
		{"paid off at the minimum", debt.MonthDebt{Paid: 5000, Minimum: 5000, Role: debt.RolePaidOff}, 5000, debt.RolePaidOff, false},
		{"skipped", debt.MonthDebt{Role: debt.RoleSkipped}, 0, debt.RoleSkipped, false},
	}
	for _, c := range cases {
		if got := minimumPart(c.x); got != c.part {
			t.Errorf("%s: minimumPart = %s, want %s", c.name, got, c.part)
		}
		if got := baseRole(c.x); got != c.role {
			t.Errorf("%s: baseRole = %q, want %q", c.name, got, c.role)
		}
		if (c.x.Extra() > 0) != c.hasLine {
			t.Errorf("%s: extra line = %v, want %v", c.name, c.x.Extra() > 0, c.hasLine)
		}
	}
}

func TestMonthDetail(t *testing.T) {
	now := d(2026, time.December, 1)
	cases := []struct {
		name string
		m    service.PlanMonth
		want string
	}{
		{"skipped", service.PlanMonth{Status: debt.MonthSkipped, Extra: 500}, "Skipped: interest added, nothing paid"},
		{"one-time", service.PlanMonth{Extra: 50000, Edited: true}, "Includes $500.00 one-time payment"},
		{"edited", service.PlanMonth{Edited: true}, "Edited payment"},
		{"edited but paid", service.PlanMonth{Edited: true, Status: debt.MonthPaid}, ""},
		{"pays off", service.PlanMonth{Next: true, Debts: []debt.MonthDebt{{Name: "Visa", Role: debt.RolePaidOff}, {Name: "Car"}, {Name: "Store", Role: debt.RolePaidOff}}}, "Pays off Visa, Store"},
		{"due now", service.PlanMonth{Next: true, Month: now}, "Due now"},
		{"next", service.PlanMonth{Next: true, Month: d(2027, 1, 1)}, "Next to pay"},
		{"plain", service.PlanMonth{Month: d(2027, 2, 1)}, ""},
	}
	for _, c := range cases {
		if got := monthDetail(c.m, now); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestEditForm(t *testing.T) {
	e := EditForm{Plan: 50000, Rows: []EditRow{
		{DebtID: 1, Name: "Visa", Minimum: 12000, WithExtra: true, Extra: "100"},
		{DebtID: 2, Name: "Car", Minimum: 25000},
		{DebtID: 3, Name: "Store", Minimum: 3500, WithExtra: true, Extra: "abc"}, // invalid: minimum only
		{DebtID: 4, Name: "Bad", Minimum: 1000, WithExtra: true, Extra: "-5"},    // negative: minimum only
	}}
	if e.MinTotal() != 41500 {
		t.Errorf("MinTotal = %s", e.MinTotal())
	}
	if e.Total() != 51500 || e.ExtraTotal() != 10000 {
		t.Errorf("Total = %s, ExtraTotal = %s", e.Total(), e.ExtraTotal())
	}
	class, txt := e.PlanStatus()
	if class != "is-more" || txt != "$15.00 more than your $500.00 plan." {
		t.Errorf("more: %q %q", class, txt)
	}
	if planStatusClass(e) != "is-more" || planStatusText(e) != txt {
		t.Error("planStatusClass/Text should match PlanStatus")
	}
	e.Plan = 51500
	if c, s := e.PlanStatus(); c != "is-match" || s != "Matches your $515.00 plan." {
		t.Errorf("match: %q %q", c, s)
	}
	e.Plan = 60000
	if c, s := e.PlanStatus(); c != "is-less" || s != "$85.00 less than your $600.00 plan." {
		t.Errorf("less: %q %q", c, s)
	}
	if choiceName(e.Rows[0]) != "choice_1" || extraName(e.Rows[1]) != "extra_2" {
		t.Error("field names")
	}
}

// ---- templ components ----

func TestMonthRowExtraLine(t *testing.T) {
	m := service.PlanMonth{Month: d(2027, time.January, 1), Next: true, Paid: 42000, Interest: 1500, Left: 500000, Debts: []debt.MonthDebt{
		{DebtID: 1, Name: "Visa", Paid: 30000, Minimum: 12000, Interest: 1000, Left: 100000, Role: debt.RoleExtra},
		{DebtID: 2, Name: "Car", Paid: 12000, Minimum: 12000, Interest: 500, Left: 400000, Role: debt.RoleMinimum},
	}}
	html := render(t, monthRow(m, true))
	txt := text(html)

	if !strings.Contains(txt, "Extra to Visa +$180.00") {
		t.Errorf("missing extra line for Visa: %s", txt)
	}
	if strings.Contains(txt, "Extra to Car") {
		t.Error("Car paid only its minimum: no extra line")
	}
	if strings.Count(html, "extra-line") != 1 {
		t.Errorf("want one extra line, got %d", strings.Count(html, "extra-line"))
	}
	// Visa's row shows the minimum part and the base role.
	if !strings.Contains(txt, "Visa $120.00 $10.00 $1,000.00 Minimum") {
		t.Errorf("visa row text: %s", txt)
	}
	if !strings.Contains(tag(html, "<details", "month-details"), "open") {
		t.Error("current month should be open with expandCurrent")
	}
	if !strings.Contains(tag(html, "<li"), "is-next") {
		t.Errorf("li = %q", tag(html, "<li"))
	}

	// Not expanded when expandCurrent is off.
	if strings.Contains(tag(render(t, monthRow(m, false)), "<details", "month-details"), "open") {
		t.Error("should be closed without expandCurrent")
	}
}

func TestMonthRowSkippedAndOneTime(t *testing.T) {
	skipped := service.PlanMonth{Month: d(2026, time.December, 1), Status: debt.MonthSkipped, Interest: 900, Left: 100900, Debts: []debt.MonthDebt{
		{DebtID: 1, Name: "Visa", Before: 100000, Interest: 900, Left: 100900, Role: debt.RoleSkipped},
	}}
	html := render(t, monthRow(skipped, true))
	if !strings.Contains(tag(html, "<li"), "is-done") {
		t.Error("skipped month should be is-done")
	}
	if strings.Contains(tag(html, "<details", "month-details"), "open") {
		t.Error("a marked month is not open")
	}
	if strings.Contains(html, "extra-line") || !strings.Contains(text(html), "Skipped: interest added, nothing paid") {
		t.Errorf("skipped text: %s", text(html))
	}
	if !strings.Contains(html, "—") {
		t.Error("skipped amounts should show a dash")
	}

	withExtra := service.PlanMonth{Month: d(2027, time.February, 1), Extra: 50000, Paid: 90000}
	html = render(t, monthRow(withExtra, false))
	if !strings.Contains(text(html), "One-time payment $500.00 on top of the regular payment") {
		t.Errorf("one-time text: %s", text(html))
	}
	if tag(html, "<form", `action="/plan/months/2027-02/remove-extra"`) == "" {
		t.Error("upcoming one-time payment should be removable")
	}
}

func TestStatusMenuPaid(t *testing.T) {
	m := service.PlanMonth{Month: d(2026, time.December, 1), Status: debt.MonthPaid, CanUndo: true}
	html := render(t, statusMenu(m))
	if !strings.Contains(tag(html, "<summary"), "is-paid") || !strings.Contains(tag(html, "<summary"), `aria-label="December 2026 payment: Paid. Change"`) {
		t.Errorf("summary = %q", tag(html, "<summary"))
	}
	if tag(html, "<form", `action="/plan/months/2026-12/undo"`) == "" {
		t.Error("Remove Paid should post to undo")
	}
	btns := buttons(html)
	if b, ok := btns["Remove Paid"]; !ok || b.disabled {
		t.Errorf("Remove Paid should be enabled: %+v", btns)
	}
	for _, label := range []string{"Skip", "Edit payment"} {
		if b, ok := btns[label]; !ok || !b.disabled {
			t.Errorf("%s should be disabled: %+v", label, btns)
		}
	}
	if strings.Contains(html, "Remove later months first") {
		t.Error("no note when it can be undone")
	}

	// An earlier paid month can't be undone.
	m.CanUndo = false
	html = render(t, statusMenu(m))
	btns = buttons(html)
	if b, ok := btns["Remove Paid"]; !ok || !b.disabled {
		t.Errorf("Remove Paid should be disabled: %+v", btns)
	}
	if tag(html, "<form") != "" {
		t.Error("no forms when nothing can be done")
	}
	if !strings.Contains(html, "Remove later months first to change this one.") {
		t.Error("missing note")
	}
}

func TestStatusMenuSkipped(t *testing.T) {
	m := service.PlanMonth{Month: d(2027, time.January, 1), Status: debt.MonthSkipped, CanUndo: true}
	html := render(t, statusMenu(m))
	btns := buttons(html)
	if b, ok := btns["Remove Skip"]; !ok || b.disabled {
		t.Errorf("Remove Skip should be enabled: %+v", btns)
	}
	if _, ok := btns["Remove Paid"]; ok {
		t.Error("skipped month should not offer Remove Paid")
	}
	for _, label := range []string{"Paid", "Edit payment"} {
		if b, ok := btns[label]; !ok || !b.disabled {
			t.Errorf("%s should be disabled: %+v", label, btns)
		}
	}
	if !strings.Contains(tag(html, "<summary"), "is-skipped") || !strings.Contains(text(html), "Skipped") {
		t.Error("summary should read Skipped")
	}
}

func TestStatusMenuCurrentAndUpcoming(t *testing.T) {
	cur := service.PlanMonth{Month: d(2027, time.March, 1), Next: true, Edited: true, Extra: 1000}
	html := render(t, statusMenu(cur))
	if !strings.Contains(text(html), "Current") || !strings.Contains(tag(html, "<summary"), "is-next") {
		t.Errorf("current summary: %q", tag(html, "<summary"))
	}
	for _, action := range []string{"paid", "skip", "remove-extra"} {
		if tag(html, "<form", `action="/plan/months/2027-03/`+action+`"`) == "" {
			t.Errorf("missing %s form", action)
		}
	}
	if tag(html, "<a", `href="/plan/months/2027-03/edit"`) == "" {
		t.Error("missing edit link")
	}
	if !strings.Contains(html, "Reset to plan") || !strings.Contains(html, `name="reset"`) {
		t.Error("edited month should offer Reset to plan")
	}
	if strings.Contains(html, "disabled") || strings.Contains(html, "Pay or skip the current month first.") {
		t.Error("current month has nothing disabled")
	}

	up := service.PlanMonth{Month: d(2027, time.April, 1)}
	html = render(t, statusMenu(up))
	btns := buttons(html)
	for _, label := range []string{"Paid", "Skip"} {
		if b, ok := btns[label]; !ok || !b.disabled {
			t.Errorf("upcoming %s should be disabled: %+v", label, btns)
		}
	}
	if tag(html, "<a", `href="/plan/months/2027-04/edit"`) == "" {
		t.Error("upcoming months can still be edited")
	}
	if strings.Contains(html, "Reset to plan") || strings.Contains(html, "Remove one-time payment") {
		t.Error("unedited month has no reset/remove")
	}
	if !strings.Contains(html, "Pay or skip the current month first.") {
		t.Error("missing note")
	}
}

type button struct{ disabled bool }

// buttons maps each <button>'s visible label to whether it's disabled.
func buttons(html string) map[string]button {
	out := map[string]button{}
	re := regexp.MustCompile(`(?s)(<button[^>]*>)(.*?)</button>`)
	for _, m := range re.FindAllStringSubmatch(html, -1) {
		out[text(m[2])] = button{disabled: strings.Contains(m[1], "disabled")}
	}
	return out
}

func TestDebtsPageInactiveBadge(t *testing.T) {
	start := d(2026, time.November, 1)
	data := DebtsData{
		Shell:        components.Shell{Active: "debts", DebtCount: 2},
		TotalCount:   2,
		Inactive:     1,
		TotalBalance: 450000,
		MinimumTotal: 12000,
		Debts: []debt.Debt{
			{ID: 1, Name: "Visa", Type: debt.TypeCard, Balance: 450000, APR: 24.99, MinimumPayment: 12000},
			{ID: 2, Name: "Old Loan", Type: debt.TypeLoan, Balance: 90000, APR: 5, MinimumPayment: 10000, Inactive: true},
		},
		Started: map[int64]debt.Debt{1: {Balance: 500000, StartDate: start}, 2: {Balance: 100000}},
		Notice:  "Saved Visa",
	}
	html := render(t, DebtsPage(data))

	if strings.Count(html, `<span class="inactive-badge">Inactive</span>`) != 1 {
		t.Errorf("want one Inactive badge, got %d", strings.Count(html, "inactive-badge"))
	}
	if !strings.Contains(tag(html, "<li", "debt-card"), "debt-card") || strings.Contains(tag(html, "<li", "debt-card"), "is-inactive") {
		t.Error("first card (Visa) should not be inactive")
	}
	if tag(html, "<li", "debt-card", "is-inactive") == "" {
		t.Error("inactive card should have is-inactive")
	}
	if tag(html, "<button", `aria-label="Activate Old Loan"`) == "" || tag(html, "<button", `aria-label="Deactivate Visa"`) == "" {
		t.Error("toggle buttons should match each debt's state")
	}
	txt := text(html)
	for _, want := range []string{
		"2 debts · $4,500.00 owed · $120.00/mo in minimums · 1 inactive, not counted",
		"Started at $5,000.00 · Nov 1, 2026",
		"Started at $1,000.00",
		"24.99%",
		"Saved Visa",
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("missing %q in %s", want, txt)
		}
	}
	if tag(html, "<div", `class="notice"`) == "" {
		t.Error("notice should render")
	}
}

func TestDebtsPageEmptyAndSearch(t *testing.T) {
	html := render(t, DebtsPage(DebtsData{}))
	if !strings.Contains(html, "No debts yet.") || strings.Contains(html, `type="search"`) {
		t.Error("empty page should show the empty state and no search")
	}
	html = render(t, DebtsPage(DebtsData{TotalCount: 3, Query: "zzz"}))
	txt := text(html)
	if !strings.Contains(txt, "0 of 3 debts match “zzz”") || !strings.Contains(txt, "No debts match your search.") {
		t.Errorf("search text: %s", txt)
	}
	if !strings.Contains(tag(html, `type="search"`), `value="zzz"`) {
		t.Error("search box should keep the query")
	}
}

func TestDebtDetailDialog(t *testing.T) {
	x := DebtDetail{
		Debt:              debt.Debt{ID: 9, Name: "Car", Type: debt.TypeLoan, Balance: 0, APR: 6.5, MinimumPayment: 25000, Inactive: true},
		Started:           debt.Debt{Balance: 800000, StartDate: d(2025, time.May, 1)},
		InterestThisMonth: 0,
		PayoffOK:          true,
	}
	html := render(t, debtDetailDialog(x))
	txt := text(html)
	if !strings.Contains(txt, "Paid off on your plan Paid off") {
		t.Errorf("paid off detail: %s", txt)
	}
	if !strings.Contains(txt, "Activate") || tag(html, "<input", `name="active"`, `value="1"`) == "" {
		t.Error("inactive debt should offer Activate")
	}

	// Active, plan not set up.
	x.Debt.Balance, x.Debt.Inactive = 100000, false
	x.InterestThisMonth, x.PayoffOK = 30000, false
	html = render(t, debtDetailDialog(x))
	txt = text(html)
	if !strings.Contains(txt, "Set up your plan") || !strings.Contains(txt, "Deactivate") {
		t.Errorf("active detail: %s", txt)
	}
	if !strings.Contains(txt, "The minimum doesn't cover the interest") {
		t.Error("should warn when the minimum doesn't cover interest")
	}
}

func TestDebtFormAndDeleteDialogs(t *testing.T) {
	ends := d(2029, time.April, 13)
	f := FormFromDebt(debt.Debt{ID: 5, Name: "Store", Type: debt.TypePromo, Balance: 100000, MinimumPayment: 2500, APR: 26.99,
		Promos: []debt.PromoItem{{Remaining: 100000, Ends: ends}}, Inactive: true}, d(2026, 11, 21))
	f.Errors = map[string]string{"name": "Name is taken"}
	html := render(t, debtFormDialog(f))
	if tag(html, "<form", `action="/debts/5"`) == "" {
		t.Error("editing form should post to /debts/5")
	}
	if !strings.Contains(html, "Name is taken") {
		t.Error("field error should show")
	}
	if !strings.Contains(html, `value="2029-04-13"`) {
		t.Error("purchase date should be filled in")
	}

	html = render(t, deleteDebtDialog(debt.Debt{ID: 5, Name: "Store"}))
	if !strings.Contains(text(html), "Delete Store?") || tag(html, "<form", `action="/debts/5/delete"`) == "" {
		t.Errorf("delete dialog: %s", text(html))
	}
}

func TestPlanPageRenders(t *testing.T) {
	// Not set up, no debts.
	html := render(t, PlanPage(PlanData{Plan: &service.TrackedPlan{}}))
	if !strings.Contains(html, "Add your debts first") {
		t.Error("empty plan should ask for debts")
	}
	// Not set up, with debts: the inline setup form.
	html = render(t, PlanPage(PlanData{Plan: &service.TrackedPlan{Owed: 1000}, Setup: SetupForm{Strategy: debt.Snowball, Pay: "extra", Extra: "50",
		Start: "2026-12", Months: []Option{{"2026-11", "November 2026"}, {"2026-12", "December 2026"}}, MinTotal: 40500}}))
	if !strings.Contains(html, "Start my plan") || !strings.Contains(tag(html, "<option", `value="snowball"`), "selected") {
		t.Error("setup form should preselect snowball")
	}
	if !strings.Contains(tag(html, "<option", `value="2026-12"`), "selected") {
		t.Error("start month should be selected")
	}
	if !strings.Contains(tag(html, `name="pay"`, `value="extra"`), "checked") {
		t.Error("Min + extra should be checked")
	}

	// Set up with months.
	plan := &service.TrackedPlan{Set: true, Strategy: debt.Avalanche, MonthlyBudget: 50000, Start: d(2026, 11, 1), StartTotal: 100000, Owed: 75000,
		PaidCount: 1, MarkedCount: 1, Changes: []debt.BudgetChange{{Month: d(2027, 6, 1), Amount: 60000}},
		Months: []service.PlanMonth{
			{Month: d(2026, 11, 1), Status: debt.MonthPaid, CanUndo: true, Paid: 25000},
			{Month: d(2026, 12, 1), Next: true, Paid: 50000, Debts: []debt.MonthDebt{{Name: "Visa", Paid: 50000, Minimum: 12000, Role: debt.RoleExtra}}},
		}}
	html = render(t, PlanPage(PlanData{Plan: plan, Rows: plan.Months, ExpandCurrent: true, Hidden: 3, Dialog: "setup",
		Setup: SetupForm{Strategy: debt.Avalanche, Locked: true, Start: "2026-11"}}))
	txt := text(html)
	for _, want := range []string{"Payoff plan", "Avalanche (highest interest first) · $500.00/mo · started November 2026",
		"Goes to $600.00/mo from June 2027", "(25%)", "Show hidden (1 paid or skipped)", "Extra to Visa +$380.00", "Plan setup"} {
		if !strings.Contains(txt, want) {
			t.Errorf("missing %q", want)
		}
	}
	if !strings.Contains(html, "The first month can't change") {
		t.Error("locked setup should explain")
	}

	// Edit dialog.
	html = render(t, PlanPage(PlanData{Plan: &service.TrackedPlan{Set: true}, Dialog: "edit", Edit: EditForm{Month: d(2027, 1, 1), Plan: 20000,
		Rows: []EditRow{{DebtID: 1, Name: "Visa", Minimum: 12000, WithExtra: true, Extra: "80", Error: "Too much"}}}}))
	if !strings.Contains(text(html), "January 2027 payment") || !strings.Contains(html, "Matches your $200.00 plan.") || !strings.Contains(html, "Too much") {
		t.Errorf("edit dialog: %s", text(html))
	}
	if !strings.Contains(html, "Everything is paid off.") {
		t.Error("no rows should say everything is paid off")
	}
}

func TestSimulateAndErrorPages(t *testing.T) {
	html := render(t, SimulatePage(SimData{Empty: true}))
	if !strings.Contains(html, "Add your debts first, then come back to try changes.") {
		t.Error("empty simulate")
	}
	html = render(t, SimulatePage(SimData{Mode: "one", Amount: "1000", Month: "1", Months: []Option{{"0", "Dec"}, {"1", "Jan"}},
		Base: SimSummary{DebtFree: d(2030, 1, 1), Months: 40, Interest: 500000}, Changed: SimSummary{DebtFree: d(2029, 6, 1), Months: 33, Interest: 400000},
		Saved: 100000, Sooner: 7, Allocs: []SimAlloc{{Name: "Visa", Amount: 100000, PaysOff: true}}, AllocAmount: 100000, AllocMonth: d(2027, 1, 1),
		Rows: []SimRow{{Month: d(2027, 1, 1), Paid: 150000, Applies: true}}}))
	txt := text(html)
	for _, want := range []string{"One-time amount", "$1,000.00 in interest", "7 months sooner", "Where your $1,000.00 goes in January 2027"} {
		if !strings.Contains(txt, want) {
			t.Errorf("simulate missing %q", want)
		}
	}
	if !strings.Contains(tag(html, "<option", `value="1"`), "selected") {
		t.Error("month 1 should be selected")
	}

	html = render(t, ErrorPage(components.Shell{}, "Something went wrong"))
	if tag(html, "<div", `class="alert"`) == "" || !strings.Contains(html, "Something went wrong") {
		t.Error("error page should show the message as an alert")
	}
}
