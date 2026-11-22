package debt

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMoneyStringFormats(t *testing.T) {
	cases := []struct {
		in   Money
		want string
	}{
		{0, "$0.00"},
		{1, "$0.01"},
		{10, "$0.10"},
		{99, "$0.99"},
		{100, "$1.00"},
		{99999, "$999.99"},
		{100000, "$1,000.00"},
		{123456, "$1,234.56"},
		{100000000, "$1,000,000.00"},
		{12345678901, "$123,456,789.01"},
		{-1, "-$0.01"},
		{-100000, "-$1,000.00"},
		{-123456789, "-$1,234,567.89"},
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Errorf("Money(%d).String() = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFromDollarsAndDollars(t *testing.T) {
	if got := FromDollars(0.1 + 0.2); got != 30 {
		t.Errorf("FromDollars(0.1+0.2) = %d, want 30", got)
	}
	if got := FromDollars(1234.565); got != 123457 && got != 123456 {
		t.Errorf("FromDollars(1234.565) = %d", got)
	}
	if got := FromDollars(-12.34); got != -1234 {
		t.Errorf("FromDollars(-12.34) = %d, want -1234", got)
	}
	if got := Money(123456).Dollars(); got != 1234.56 {
		t.Errorf("Dollars() = %v, want 1234.56", got)
	}
}

func TestParseDollars(t *testing.T) {
	ok := []struct {
		in   string
		want Money
	}{
		{"", 0},
		{"   ", 0},
		{"$", 0},
		{"0", 0},
		{"99", 9900},
		{"$99", 9900},
		{"1,234.56", 123456},
		{"$1,234.56", 123456},
		{" 1 234.5 ", 123450},
		{"$ 12.3", 1230},
		{"0.005", 1}, // rounds to the nearest cent
		{"0.004", 0},
		{"1000000", 100000000},
	}
	for _, c := range ok {
		got, err := ParseDollars(c.in)
		if err != nil {
			t.Errorf("ParseDollars(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseDollars(%q) = %d, want %d", c.in, got, c.want)
		}
	}

	bad := []string{"abc", "12abc", "1.2.3", "NaN", "nan", "Inf", "+Inf", "-Inf", "infinity", "--5", "$$x"}
	for _, in := range bad {
		got, err := ParseDollars(in)
		if err == nil {
			t.Errorf("ParseDollars(%q) = %d, want error", in, got)
			continue
		}
		if got != 0 {
			t.Errorf("ParseDollars(%q) returned %d with error, want 0", in, got)
		}
		if !strings.Contains(err.Error(), "not a valid amount") {
			t.Errorf("ParseDollars(%q) error = %q", in, err)
		}
	}
}

func TestMoneyJSON(t *testing.T) {
	cases := map[Money]string{0: "0.00", 5: "0.05", 123456: "1234.56", -1050: "-10.50"}
	for m, want := range cases {
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != want {
			t.Errorf("Marshal(%d) = %s, want %s", m, b, want)
		}
		var back Money
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatal(err)
		}
		if back != m {
			t.Errorf("round trip %d -> %s -> %d", m, b, back)
		}
	}

	// In a struct, alongside other fields.
	type wrap struct {
		Amount Money `json:"amount"`
	}
	b, _ := json.Marshal(wrap{Amount: 9999})
	if string(b) != `{"amount":99.99}` {
		t.Errorf("struct marshal = %s", b)
	}
	var w wrap
	if err := json.Unmarshal([]byte(`{"amount": 12.345}`), &w); err != nil || w.Amount != 1235 && w.Amount != 1234 {
		t.Errorf("unmarshal 12.345 = %d, %v", w.Amount, err)
	}
	if err := json.Unmarshal([]byte(`{"amount": 7}`), &w); err != nil || w.Amount != 700 {
		t.Errorf("unmarshal 7 = %d, %v", w.Amount, err)
	}

	var m Money
	if err := json.Unmarshal([]byte(`"12.00"`), &m); err == nil {
		t.Error("a JSON string should be rejected")
	} else if !strings.Contains(err.Error(), "number of dollars") {
		t.Errorf("error = %v", err)
	}
	if err := m.UnmarshalJSON([]byte("abc")); err == nil {
		t.Error("UnmarshalJSON(abc) should fail")
	}
}

func TestMonthlyInterest(t *testing.T) {
	cases := []struct {
		bal  Money
		apr  float64
		want Money
	}{
		{0, 24.99, 0},
		{100000, 0, 0},
		{120000, 12, 1200},     // $1,200 at 12% = $12.00
		{100000, 18, 1500},     // $1,000 at 18% = $15.00
		{620000, 24.99, 12912}, // $129.115 rounds up
		{10000, 5, 42},         // $0.4166.. rounds down
		{10000, 7, 58},         // $0.5833.. rounds up
		{3000, 20, 50},         // exactly $0.50
	}
	for _, c := range cases {
		if got := MonthlyInterest(c.bal, c.apr); got != c.want {
			t.Errorf("MonthlyInterest(%s, %v) = %d, want %d", c.bal, c.apr, got, c.want)
		}
	}
}

func TestMinMoney(t *testing.T) {
	if minMoney(3, 5) != 3 || minMoney(5, 3) != 3 || minMoney(-1, 0) != -1 {
		t.Error("minMoney picks the smaller value")
	}
}

func TestThemeValid(t *testing.T) {
	for _, th := range []Theme{ThemeDark, ThemeLight} {
		if !th.Valid() {
			t.Errorf("%q should be valid", th)
		}
	}
	for _, th := range []Theme{"", "Dark", "blue", "system"} {
		if th.Valid() {
			t.Errorf("%q should be invalid", th)
		}
	}
}

func TestStrategyValidAndLabel(t *testing.T) {
	labels := map[Strategy]string{Avalanche: "Avalanche", Snowball: "Snowball", MinimumOnly: "Minimums only", Custom: "Custom"}
	for s, want := range labels {
		if !s.Valid() {
			t.Errorf("%q should be valid", s)
		}
		if got := s.Label(); got != want {
			t.Errorf("%q.Label() = %q, want %q", s, got, want)
		}
	}
	for _, s := range []Strategy{"", "Avalanche", "fastest"} {
		if s.Valid() {
			t.Errorf("%q should be invalid", s)
		}
	}
	if got := Strategy("other").Label(); got != "other" {
		t.Errorf("unknown label = %q", got)
	}
	if len(PlanTypes) != 4 || PlanTypes[3] != Custom {
		t.Errorf("PlanTypes = %v", PlanTypes)
	}
}

func TestValidType(t *testing.T) {
	for _, ty := range Types {
		if !ValidType(ty) {
			t.Errorf("%q should be valid", ty)
		}
	}
	for _, ty := range []string{"", "credit card", "Mortgage"} {
		if ValidType(ty) {
			t.Errorf("%q should be invalid", ty)
		}
	}
	if DefaultType != TypeCard {
		t.Errorf("DefaultType = %q", DefaultType)
	}
}

func TestOrderNormalizeAndDescribe(t *testing.T) {
	cases := []struct {
		in       Order
		norm     Order
		describe string
	}{
		{Order{}, Order{OrderByInterest, OrderHighFirst}, "highest interest first"},
		{Order{By: "junk", First: "junk"}, Order{OrderByInterest, OrderHighFirst}, "highest interest first"},
		{Order{By: OrderByBalance}, Order{OrderByBalance, OrderHighFirst}, "highest balance first"},
		{Order{First: OrderLowFirst}, Order{OrderByInterest, OrderLowFirst}, "lowest interest first"},
		{Order{OrderByBalance, OrderLowFirst}, Order{OrderByBalance, OrderLowFirst}, "lowest balance first"},
		{Order{OrderByInterest, OrderHighFirst}, Order{OrderByInterest, OrderHighFirst}, "highest interest first"},
	}
	for _, c := range cases {
		if got := c.in.Normalize(); got != c.norm {
			t.Errorf("%+v.Normalize() = %+v, want %+v", c.in, got, c.norm)
		}
		if got := c.in.Describe(); got != c.describe {
			t.Errorf("%+v.Describe() = %q, want %q", c.in, got, c.describe)
		}
	}
}

func TestPromoTotal(t *testing.T) {
	if got := (Debt{}).PromoTotal(); got != 0 {
		t.Errorf("no promos = %d", got)
	}
	d := Debt{Promos: []PromoItem{{Remaining: 10000}, {Remaining: 2550}, {Remaining: 0}}}
	if got := d.PromoTotal(); got != 12550 {
		t.Errorf("PromoTotal = %d, want 12550", got)
	}
}

func TestNextPromoEnd(t *testing.T) {
	today := time.Date(2026, time.November, 21, 0, 0, 0, 0, time.UTC)
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

	if got := (Debt{}).NextPromoEnd(today); got != nil {
		t.Errorf("no promos = %v", got)
	}

	d := Debt{Promos: []PromoItem{
		{Remaining: 500, Ends: day(2027, time.June, 1)},
		{Remaining: 0, Ends: day(2027, time.January, 1)},     // paid: ignored
		{Remaining: 500, Ends: day(2026, time.October, 1)},   // expired: ignored
		{Remaining: 500, Ends: today},                        // ends today: not after today
		{Remaining: 500, Ends: day(2027, time.March, 15)},    // earliest owing
		{Remaining: 500, Ends: day(2028, time.December, 31)}, // later
	}}
	got := d.NextPromoEnd(today)
	if got == nil || !got.Equal(day(2027, time.March, 15)) {
		t.Fatalf("NextPromoEnd = %v, want 2027-03-15", got)
	}
	// It points into the debt's own slice.
	if got != &d.Promos[4].Ends {
		t.Error("NextPromoEnd should return a pointer to the promo's Ends")
	}

	allDone := Debt{Promos: []PromoItem{{Remaining: 0, Ends: day(2027, time.June, 1)}, {Remaining: 10, Ends: day(2020, 1, 1)}}}
	if got := allDone.NextPromoEnd(today); got != nil {
		t.Errorf("all paid or expired = %v", got)
	}
}

func TestInterestFreeBranches(t *testing.T) {
	today := time.Date(2026, time.November, 21, 0, 0, 0, 0, time.UTC)
	future := today.AddDate(0, 3, 0)
	past := today.AddDate(0, -3, 0)
	cases := []struct {
		name string
		d    Debt
		want bool
	}{
		{"card", Debt{Type: TypeCard, TermEnds: &future}, false},
		{"promo term ahead", Debt{Type: TypePromo, TermEnds: &future}, true},
		{"promo term past", Debt{Type: TypePromo, TermEnds: &past}, false},
		{"promo no term", Debt{Type: TypePromo}, false},
		{"promo purchases owing", Debt{Type: TypePromo, TermEnds: &past, Promos: []PromoItem{{Remaining: 1, Ends: future}}}, true},
		{"promo purchases expired", Debt{Type: TypePromo, TermEnds: &future, Promos: []PromoItem{{Remaining: 1, Ends: past}}}, false},
	}
	for _, c := range cases {
		if got := c.d.InterestFree(today); got != c.want {
			t.Errorf("%s: InterestFree = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPaymentsBefore(t *testing.T) {
	today := time.Date(2026, time.November, 21, 0, 0, 0, 0, time.UTC)
	end := time.Date(2027, time.March, 10, 0, 0, 0, 0, time.UTC)
	// Due on the 5th: Dec 5, Jan 5, Feb 5, Mar 5.
	if got := PaymentsBefore(end, today, 5); got != 4 {
		t.Errorf("due 5th = %d, want 4", got)
	}
	// Due on the 25th: Nov 25, Dec 25, Jan 25, Feb 25.
	if got := PaymentsBefore(end, today, 25); got != 4 {
		t.Errorf("due 25th = %d, want 4", got)
	}
	// A due date on the end day doesn't count.
	if got := PaymentsBefore(end, today, 10); got != 3 {
		t.Errorf("due 10th = %d, want 3", got)
	}
	// Due on the 31st uses the last day of short months.
	if got := PaymentsBefore(time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC), today, 31); got != 4 {
		t.Errorf("due 31st = %d, want 4 (Nov 30, Dec 31, Jan 31, Feb 28)", got)
	}
	// No due day falls back to whole months.
	if got, want := PaymentsBefore(end, today, 0), MonthsBefore(&end, today); got != want {
		t.Errorf("no due day = %d, want MonthsBefore %d", got, want)
	}
	if got := PaymentsBefore(today, today, 5); got != 0 {
		t.Errorf("end today = %d, want 0", got)
	}
}

func TestMonthRecordMarked(t *testing.T) {
	if (MonthRecord{}).Marked() {
		t.Error("empty status should not be marked")
	}
	for _, s := range []string{MonthPaid, MonthSkipped} {
		if !(MonthRecord{Status: s}).Marked() {
			t.Errorf("%q should be marked", s)
		}
	}
}

func TestMonthDebtExtra(t *testing.T) {
	cases := []struct {
		paid, min, want Money
	}{
		{30000, 10000, 20000},
		{10000, 10000, 0},
		{5000, 10000, 0}, // paid less than the minimum: never negative
		{0, 0, 0},
	}
	for _, c := range cases {
		if got := (MonthDebt{Paid: c.paid, Minimum: c.min}).Extra(); got != c.want {
			t.Errorf("Extra(paid %d, min %d) = %d, want %d", c.paid, c.min, got, c.want)
		}
	}
}

func TestMinimumTotal(t *testing.T) {
	if got := MinimumTotal(nil); got != 0 {
		t.Errorf("nil = %d", got)
	}
	if got := MinimumTotal(sampleDebts()); got != FromDollars(405) {
		t.Errorf("MinimumTotal = %s, want $405.00", got)
	}
}

func TestDefaultPreferences(t *testing.T) {
	p := DefaultPreferences
	if p.Theme != ThemeDark || !p.ExpandCurrent || p.NoticeSeconds != 5 {
		t.Errorf("DefaultPreferences = %+v", p)
	}
	found := false
	for _, n := range NoticeChoices {
		if n == p.NoticeSeconds {
			found = true
		}
	}
	if !found {
		t.Errorf("default notice %d not among choices %v", p.NoticeSeconds, NoticeChoices)
	}
}

func TestValidate(t *testing.T) {
	good := Debt{Name: "Visa", Balance: 1000, APR: 10, MinimumPayment: 100, Type: TypeCard, DueDay: 15}
	if err := good.Validate(); err != nil {
		t.Fatalf("good debt: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*Debt)
		want   string
	}{
		{"no name", func(d *Debt) { d.Name = "" }, "name is required"},
		{"zero balance", func(d *Debt) { d.Balance = 0 }, "balance must be greater than zero"},
		{"negative balance", func(d *Debt) { d.Balance = -1 }, "balance must be greater than zero"},
		{"negative apr", func(d *Debt) { d.APR = -0.1 }, "apr cannot be negative"},
		{"zero minimum", func(d *Debt) { d.MinimumPayment = 0 }, "minimum payment"},
		{"bad type", func(d *Debt) { d.Type = "Mortgage" }, "unknown type"},
		{"negative promo", func(d *Debt) { d.Type = TypePromo; d.Promos = []PromoItem{{Remaining: -5}} }, "negative amount"},
		{"due day 32", func(d *Debt) { d.DueDay = 32 }, "due day"},
		{"due day -1", func(d *Debt) { d.DueDay = -1 }, "due day"},
	}
	for _, c := range cases {
		d := good
		c.mutate(&d)
		err := d.Validate()
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want containing %q", c.name, err, c.want)
		}
	}
	// Empty type is allowed, as is due day 0 (not set) and 31.
	for _, f := range []func(*Debt){func(d *Debt) { d.Type = "" }, func(d *Debt) { d.DueDay = 0 }, func(d *Debt) { d.DueDay = 31 }} {
		d := good
		f(&d)
		if err := d.Validate(); err != nil {
			t.Errorf("%+v: %v", d, err)
		}
	}
}

func TestPlanRejectsZeroBalanceAndUnknownStrategy(t *testing.T) {
	calc := NewCalculator()
	debts := sampleDebts()
	debts[1].Balance = 0 // a paid-off debt must be left out by the caller
	_, err := calc.Plan(PlanRequest{Debts: debts, MonthlyBudget: FromDollars(800), Strategy: Avalanche, StartDate: start})
	if err == nil || !strings.Contains(err.Error(), "Visa: balance must be greater than zero") {
		t.Errorf("zero balance err = %v", err)
	}
	_, err = calc.Plan(PlanRequest{Debts: sampleDebts(), Strategy: "fastest", StartDate: start})
	if err == nil || !strings.Contains(err.Error(), "unknown strategy") {
		t.Errorf("unknown strategy err = %v", err)
	}
	_, err = calc.Plan(PlanRequest{Debts: sampleDebts(), MonthlyBudget: FromDollars(100), Strategy: Avalanche, StartDate: start})
	if !errors.Is(err, ErrBudgetTooLow) || !strings.Contains(err.Error(), "$405.00") {
		t.Errorf("budget too low err = %v", err)
	}
}

func TestPlanZeroBudgetUsesMinimumsAndDefaults(t *testing.T) {
	calc := &Calculator{} // MaxMonths 0 falls back to the default
	p, err := calc.Plan(PlanRequest{Debts: sampleDebts(), Strategy: Avalanche, StartDate: start})
	if err != nil {
		t.Fatal(err)
	}
	if p.MonthlyBudget != FromDollars(405) {
		t.Errorf("budget = %s, want the minimums $405.00", p.MonthlyBudget)
	}
	// MinimumOnly ignores the budget it's given.
	p, err = calc.Plan(PlanRequest{Debts: sampleDebts(), MonthlyBudget: FromDollars(5000), Strategy: MinimumOnly, StartDate: start})
	if err != nil {
		t.Fatal(err)
	}
	if p.MonthlyBudget != FromDollars(405) {
		t.Errorf("minimum-only budget = %s, want $405.00", p.MonthlyBudget)
	}
	if !p.StartDate.Equal(time.Date(2026, time.November, 21, 0, 0, 0, 0, time.Local)) {
		t.Errorf("start = %s", p.StartDate)
	}
}

func TestPlanMaxMonths(t *testing.T) {
	calc := &Calculator{MaxMonths: 3}
	_, err := calc.Plan(PlanRequest{Debts: sampleDebts(), MonthlyBudget: FromDollars(800), Strategy: Avalanche, StartDate: start})
	if !errors.Is(err, ErrNeverPaidOff) {
		t.Errorf("err = %v, want ErrNeverPaidOff", err)
	}
}

func TestMinimumOnlyDoesNotRollFreedMinimums(t *testing.T) {
	// The small debt is paid off in month one; its freed minimum must not
	// go to the other debt on a minimum-only plan.
	debts := []Debt{
		{ID: 1, Name: "Small", Balance: 5000, APR: 0, MinimumPayment: 5000},
		{ID: 2, Name: "Big", Balance: 100000, APR: 0, MinimumPayment: 10000},
	}
	p, err := NewCalculator().Plan(PlanRequest{Debts: debts, Strategy: MinimumOnly, StartDate: start})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range p.Schedule[1:] {
		if m.TotalPaid > 10000 {
			t.Errorf("month %d paid %s; freed minimum rolled over", m.Number, m.TotalPaid)
		}
	}
	if p.Months != 10 {
		t.Errorf("months = %d, want 10", p.Months)
	}

	// Avalanche with the same budget does roll it over.
	a, err := NewCalculator().Plan(PlanRequest{Debts: debts, MonthlyBudget: 15000, Strategy: Avalanche, StartDate: start})
	if err != nil {
		t.Fatal(err)
	}
	if a.Months >= p.Months {
		t.Errorf("avalanche months %d should beat minimum-only %d", a.Months, p.Months)
	}
}

func TestPrioritizeTieBreaks(t *testing.T) {
	acct := func(name string, bal Money, apr float64) *account {
		return &account{Debt: Debt{Name: name, APR: apr}, balance: bal}
	}
	names := func(as []*account) string {
		var n []string
		for _, a := range as {
			n = append(n, a.Name)
		}
		return strings.Join(n, ",")
	}
	cases := []struct {
		name     string
		accounts []*account
		s        Strategy
		o        Order
		want     string
	}{
		{"paid off left out", []*account{acct("Done", 0, 30), acct("Open", 100, 1)}, Avalanche, Order{}, "Open"},
		{"avalanche APR tie -> smaller balance", []*account{acct("Big", 500, 10), acct("Small", 100, 10)}, Avalanche, Order{}, "Small,Big"},
		{"snowball balance tie -> higher APR", []*account{acct("A", 100, 5), acct("B", 100, 15)}, Snowball, Order{}, "B,A"},
		{"custom balance tie -> higher APR", []*account{acct("Low", 100, 5), acct("High", 100, 20)}, Custom, Order{By: OrderByBalance, First: OrderLowFirst}, "High,Low"},
		{"custom balance low first", []*account{acct("Big", 500, 5), acct("Small", 100, 5)}, Custom, Order{By: OrderByBalance, First: OrderLowFirst}, "Small,Big"},
		{"custom APR tie -> smaller balance", []*account{acct("Big", 500, 10), acct("Small", 100, 10)}, Custom, Order{}, "Small,Big"},
		{"custom interest low first", []*account{acct("Hi", 100, 20), acct("Lo", 100, 5)}, Custom, Order{First: OrderLowFirst}, "Lo,Hi"},
		{"minimum-only extra uses avalanche", []*account{acct("Lo", 100, 5), acct("Hi", 900, 20)}, MinimumOnly, Order{}, "Hi,Lo"},
	}
	for _, c := range cases {
		if got := names(prioritize(c.accounts, c.s, c.o)); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestAllocateLeftoverGoesByStrategy(t *testing.T) {
	debts := []Debt{
		{ID: 1, Name: "Tiny", Balance: 2000, APR: 0, MinimumPayment: 1000},
		{ID: 2, Name: "Card", Balance: 100000, APR: 20, MinimumPayment: 2500},
	}
	// Allocate $100 to Tiny (owes $20); the $80 left goes to Card by strategy.
	p, err := NewCalculator().Plan(PlanRequest{Debts: debts, MonthlyBudget: 12500, Strategy: Avalanche, StartDate: start,
		Allocate: func(period int) map[int64]Money {
			if period == 0 {
				return map[int64]Money{1: 10000}
			}
			return nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Payment{}
	for _, pay := range p.Schedule[0].Payments {
		got[pay.Debt] = pay
	}
	if got["Tiny"].Amount != 2000 || !got["Tiny"].PaidOff {
		t.Errorf("Tiny = %+v, want $20 and paid off", got["Tiny"])
	}
	if got["Card"].Amount != 10500 {
		t.Errorf("Card paid %s, want $105.00 (budget less Tiny's $20)", got["Card"].Amount)
	}
}

func TestCompareSkipsNeverEndingBaseline(t *testing.T) {
	// The minimum doesn't cover interest, but a bigger budget does.
	debts := []Debt{{ID: 1, Name: "Card", Balance: FromDollars(10000), APR: 24, MinimumPayment: FromDollars(150)}}
	plans, err := NewCalculator().Compare(PlanRequest{Debts: debts, MonthlyBudget: FromDollars(1000), StartDate: start})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 {
		t.Fatalf("got %d plans, want 2 (minimum-only dropped)", len(plans))
	}
	for _, p := range plans {
		if p.Strategy == MinimumOnly {
			t.Error("minimum-only should be dropped")
		}
	}

	_, err = NewCalculator().Compare(PlanRequest{Debts: debts, MonthlyBudget: FromDollars(10), StartDate: start})
	if !errors.Is(err, ErrBudgetTooLow) || !strings.HasPrefix(err.Error(), "Avalanche: ") {
		t.Errorf("err = %v, want Avalanche-prefixed ErrBudgetTooLow", err)
	}
	if _, err := NewCalculator().Compare(PlanRequest{}); !errors.Is(err, ErrNoDebts) {
		t.Errorf("no debts err = %v", err)
	}
}

func TestDueDateDefaultsAndShortMonths(t *testing.T) {
	// No due day is treated as the 1st; start on the 21st means next month.
	if got := dueDate(start, 0, 0); got.Month() != time.December || got.Day() != 1 {
		t.Errorf("due 0 = %s, want Dec 1", got.Format("2006-01-02"))
	}
	// Due 30th in February (non leap year 2027) uses Feb 28.
	jan := time.Date(2027, time.January, 15, 0, 0, 0, 0, time.Local)
	if got := dueDate(jan, 1, 30); got.Month() != time.February || got.Day() != 28 {
		t.Errorf("Feb 30 = %s, want Feb 28", got.Format("2006-01-02"))
	}
	// Leap year 2028.
	jan28 := time.Date(2028, time.January, 15, 0, 0, 0, 0, time.Local)
	if got := dueDate(jan28, 1, 31); got.Day() != 29 {
		t.Errorf("leap Feb = %s, want Feb 29", got.Format("2006-01-02"))
	}
	// Due on the start day: period 0 is the start day itself.
	if got := dueDate(start, 0, 21); !got.Equal(start) {
		t.Errorf("due on start day = %s", got.Format("2006-01-02"))
	}
}

func TestLumpSumBestEmpty(t *testing.T) {
	if (&LumpSumAnalysis{}).Best() != nil {
		t.Error("Best on no options should be nil")
	}
	a := &LumpSumAnalysis{Options: []LumpSumOption{{Target: "x"}, {Target: "y"}}}
	if b := a.Best(); b == nil || b.Target != "x" {
		t.Errorf("Best = %+v", b)
	}
}

func TestLumpSumPlanError(t *testing.T) {
	if _, err := NewCalculator().LumpSum(PlanRequest{}, 100); !errors.Is(err, ErrNoDebts) {
		t.Errorf("err = %v, want ErrNoDebts", err)
	}
	if _, err := NewCalculator().LumpSum(PlanRequest{Debts: sampleDebts(), StartDate: start}, -5); !errors.Is(err, ErrInvalidAmount) {
		t.Errorf("negative amount err = %v", err)
	}
}

func TestLumpSumCapsAtOwed(t *testing.T) {
	debts := []Debt{
		{ID: 1, Name: "A", Balance: 10000, APR: 10, MinimumPayment: 2500},
		{ID: 2, Name: "B", Balance: 20000, APR: 20, MinimumPayment: 2500},
	}
	a, err := NewCalculator().LumpSum(PlanRequest{Debts: debts, MonthlyBudget: 10000, Strategy: Avalanche, StartDate: start}, 1000000)
	if err != nil {
		t.Fatal(err)
	}
	if a.Applied != 30000 || a.Amount != 1000000 {
		t.Errorf("applied %s of %s, want $300.00", a.Applied, a.Amount)
	}
	for _, o := range a.Options {
		if len(o.PaidOff) != 2 || o.FreedMinimum != 5000 || o.Outcome.Months != 0 {
			t.Errorf("option %s = %+v", o.Target, o)
		}
		if o.Outcome.TotalPaid != 30000 {
			t.Errorf("option %s total paid %s, want $300.00", o.Target, o.Outcome.TotalPaid)
		}
	}
	if !a.Options[0].Recommended || a.Options[1].Recommended {
		t.Error("only the first option is recommended")
	}
}
