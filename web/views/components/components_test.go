package components

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"

	"github.com/lee5i3/debt-reducer/internal/debt"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// tag returns the first opening tag that contains all the given fragments.
func tag(html string, fragments ...string) string {
	for _, m := range regexp.MustCompile(`<[a-zA-Z][^>]*>`).FindAllString(html, -1) {
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

func TestShellThemeName(t *testing.T) {
	cases := map[debt.Theme]string{"": "dark", debt.ThemeDark: "dark", debt.ThemeLight: "light", "blue": "dark"}
	for in, want := range cases {
		if got := (Shell{Theme: in}).ThemeName(); got != want {
			t.Errorf("ThemeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShellSettingsAndCloseURL(t *testing.T) {
	cases := []struct {
		here, settings, close string
	}{
		{"", "/?settings=1", "/"},
		{"/", "/?settings=1", "/"},
		{"/debts", "/debts?settings=1", "/debts"},
		{"/debts?q=visa", "/debts?q=visa&settings=1", "/debts?q=visa"},
		{"/simulate?mode=one&amount=5", "/simulate?mode=one&amount=5&settings=1", "/simulate?mode=one&amount=5"},
	}
	for _, c := range cases {
		s := Shell{Here: c.here}
		if got := s.SettingsURL(); got != c.settings {
			t.Errorf("SettingsURL(%q) = %q, want %q", c.here, got, c.settings)
		}
		if got := s.CloseURL(); got != c.close {
			t.Errorf("CloseURL(%q) = %q, want %q", c.here, got, c.close)
		}
	}
}

func TestNoticeLabel(t *testing.T) {
	cases := map[int]string{0: "Never", 3: "3 seconds", 5: "5 seconds", 30: "30 seconds"}
	for in, want := range cases {
		if got := noticeLabel(in); got != want {
			t.Errorf("noticeLabel(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestCurrent(t *testing.T) {
	if Current(true) != "page" || Current(false) != "false" {
		t.Error("Current should be page/false")
	}
}

func TestAlerts(t *testing.T) {
	if got := strings.TrimSpace(render(t, Alerts("", ""))); got != "" {
		t.Errorf("no messages should render nothing, got %q", got)
	}

	errOnly := render(t, Alerts("Something <broke>", ""))
	alert := tag(errOnly, `class="alert"`)
	if alert == "" || !strings.Contains(alert, `role="alert"`) {
		t.Fatalf("missing alert div: %s", errOnly)
	}
	if strings.Contains(errOnly, "data-autoclose") {
		t.Error("errors must not close on their own")
	}
	if tag(errOnly, "banner-close", "data-dismiss") == "" {
		t.Error("error should have a close button")
	}
	if !strings.Contains(errOnly, "Something &lt;broke&gt;") {
		t.Error("error text should be escaped")
	}
	if strings.Contains(errOnly, `class="notice"`) {
		t.Error("no notice expected")
	}

	noticeOnly := render(t, Alerts("", "Saved Visa"))
	notice := tag(noticeOnly, `class="notice"`)
	if notice == "" || !strings.Contains(notice, "data-autoclose") || !strings.Contains(notice, `role="status"`) {
		t.Fatalf("notice div = %q", notice)
	}
	if tag(noticeOnly, "banner-close", "data-dismiss") == "" {
		t.Error("notice should have a close button")
	}
	if !strings.Contains(noticeOnly, "Saved Visa") || strings.Contains(noticeOnly, `class="alert"`) {
		t.Errorf("notice output = %s", noticeOnly)
	}

	both := render(t, Alerts("Bad", "Good"))
	if strings.Index(both, `class="alert"`) > strings.Index(both, `class="notice"`) {
		t.Error("error should come before the notice")
	}
	if strings.Count(both, "data-dismiss") != 2 {
		t.Error("each banner should have its own close button")
	}
}

func TestModal(t *testing.T) {
	html := render(t, Modal("t-id", "alertdialog", "wide", "/debts"))
	d := tag(html, "<dialog")
	for _, want := range []string{`class="modal wide"`, "open", `role="alertdialog"`, `aria-labelledby="t-id"`, `data-close="/debts"`} {
		if !strings.Contains(d, want) {
			t.Errorf("dialog tag %q missing %s", d, want)
		}
	}
}

func TestSettingsDialog(t *testing.T) {
	s := Shell{Here: "/debts?q=x", Settings: &debt.Preferences{Theme: debt.ThemeLight, ExpandCurrent: false, NoticeSeconds: 10}}
	html := render(t, SettingsDialog(s))

	light := tag(html, `name="theme"`, `value="light"`)
	dark := tag(html, `name="theme"`, `value="dark"`)
	if !strings.Contains(light, "checked") || strings.Contains(dark, "checked") {
		t.Errorf("light should be checked: light=%q dark=%q", light, dark)
	}
	if strings.Contains(tag(html, `name="expand_current"`), "checked") {
		t.Error("expand_current should be unchecked")
	}
	if sel := tag(html, "<option", `value="10"`); !strings.Contains(sel, "selected") {
		t.Errorf("10 seconds should be selected: %q", sel)
	}
	if strings.Count(html, "selected") != 1 {
		t.Errorf("exactly one option should be selected, got %d", strings.Count(html, "selected"))
	}
	for _, n := range debt.NoticeChoices {
		if !strings.Contains(html, ">"+noticeLabel(n)+"</option>") {
			t.Errorf("missing option %q", noticeLabel(n))
		}
	}
	if !strings.Contains(tag(html, `name="back"`), `value="/debts?q=x"`) {
		t.Errorf("back field = %q", tag(html, `name="back"`))
	}
	if !strings.Contains(tag(html, "<dialog"), `data-close="/debts?q=x"`) {
		t.Error("dialog should close back to the current page")
	}
	if !strings.Contains(tag(html, "<form"), `action="/settings"`) {
		t.Error("form should post to /settings")
	}

	// Dark theme, expand on, "Never".
	s.Settings = &debt.Preferences{Theme: debt.ThemeDark, ExpandCurrent: true, NoticeSeconds: 0}
	s.SettingsError = "Pick a theme"
	html = render(t, SettingsDialog(s))
	if !strings.Contains(tag(html, `name="theme"`, `value="dark"`), "checked") || strings.Contains(tag(html, `name="theme"`, `value="light"`), "checked") {
		t.Error("dark should be checked")
	}
	if !strings.Contains(tag(html, `name="expand_current"`), "checked") {
		t.Error("expand_current should be checked")
	}
	if sel := tag(html, "<option", `value="0"`); !strings.Contains(sel, "selected") {
		t.Errorf("Never should be selected: %q", sel)
	}
	if !strings.Contains(html, "Pick a theme") || tag(html, `class="alert"`) == "" {
		t.Error("settings error should show as an alert")
	}
}

func TestLayout(t *testing.T) {
	s := Shell{Active: "debts", Theme: debt.ThemeLight, NoticeSeconds: 10, DebtCount: 3, ActiveDebts: 2, TotalOwed: 1234567,
		DebtFree: time.Date(2029, time.July, 1, 0, 0, 0, 0, time.UTC), InterestPaid: 5000, InterestAhead: 20000, Here: "/debts"}
	html := render(t, Layout(s, "Debts · Debt Reducer"))

	if !strings.Contains(tag(html, "<html"), `data-theme="light"`) {
		t.Errorf("html tag = %q", tag(html, "<html"))
	}
	if !strings.Contains(tag(html, `name="color-scheme"`), `content="light"`) {
		t.Error("color-scheme meta should match the theme")
	}
	if !strings.Contains(tag(html, "<body"), `data-notice-seconds="10"`) {
		t.Errorf("body tag = %q", tag(html, "<body"))
	}
	if !strings.Contains(html, "<title>Debts · Debt Reducer</title>") {
		t.Error("missing title")
	}
	if !strings.Contains(tag(html, `href="/debts"`, "nav-item"), `aria-current="page"`) {
		t.Error("debts nav should be current")
	}
	if !strings.Contains(tag(html, `href="/"`, "nav-item"), `aria-current="false"`) {
		t.Error("plan nav should not be current")
	}
	for _, want := range []string{`<span class="badge">3</span>`, "$12,345.67", "Debt free Jul 2029", "$200.00 interest to come", "$50.00 interest paid"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q", want)
		}
	}
	if tag(html, `href="/debts?settings=1"`, "nav-item") == "" {
		t.Error("sidebar should link to settings over the current page")
	}
	if strings.Contains(html, "topbar") || strings.Contains(html, `class="logo"`) {
		t.Error("the top bar and logo are gone")
	}
	if strings.Contains(html, "settings-title") {
		t.Error("settings dialog should be closed without Settings")
	}
	if !strings.Contains(html, `<footer class="app-footer">Debt Reducer (dev build)</footer>`) {
		t.Error("missing name/version footer")
	}
	if strings.Contains(html, `aria-disabled="true"`) {
		t.Error("plan and simulate should be links when there are active debts")
	}

	// Dark default, no debts, settings open, notices never close.
	s = Shell{Active: "plan", NoticeSeconds: 0, Settings: &debt.Preferences{Theme: debt.ThemeDark, NoticeSeconds: 0}}
	html = render(t, Layout(s, "Plan"))
	if !strings.Contains(tag(html, "<html"), `data-theme="dark"`) {
		t.Error("default theme should be dark")
	}
	if !strings.Contains(tag(html, "<body"), `data-notice-seconds="0"`) {
		t.Error("notice seconds 0 should render")
	}
	if strings.Contains(html, "side-summary") || strings.Contains(html, `class="badge"`) {
		t.Error("no debt summary without debts")
	}
	if !strings.Contains(html, `id="settings-title"`) {
		t.Error("settings dialog should be open")
	}
	if !strings.Contains(tag(html, `settings=1"`, "nav-item"), `aria-current="page"`) {
		t.Error("settings link should be current while open")
	}
}

func TestMoneyInput(t *testing.T) {
	html := render(t, MoneyInput(MoneyField{ID: "amt", Name: "amount", Value: "1234.5", Invalid: true, Short: true,
		Attrs: templ.Attributes{"autofocus": true}}))
	in := tag(html, "<input")
	for _, want := range []string{`id="amt"`, `name="amount"`, `value="1,234.50"`, `inputmode="decimal"`, `data-format="money"`, `aria-invalid="true"`, "autofocus"} {
		if !strings.Contains(in, want) {
			t.Errorf("input %q missing %s", in, want)
		}
	}
	if !strings.Contains(tag(html, "<span", "money-input"), "short") {
		t.Error("short class missing")
	}
	html = render(t, MoneyInput(MoneyField{Name: "x"}))
	if strings.Contains(tag(html, "<input"), " id=") || !strings.Contains(tag(html, "<input"), `aria-invalid="false"`) {
		t.Errorf("plain input = %q", tag(html, "<input"))
	}
}

func TestIcons(t *testing.T) {
	if got := render(t, Icon("trash")); got != `<i class="ph ph-trash icon" aria-hidden="true"></i>` {
		t.Errorf("Icon = %q", got)
	}
	named := []struct {
		c    templ.Component
		name string
	}{
		{IconLogo(), "chart-line-down"}, {IconPlan(), "chart-line"}, {IconBolt(), "lightning"}, {IconCard(), "credit-card"},
		{IconSearch(), "magnifying-glass"}, {IconPlus(), "plus"}, {IconPencil(), "pencil-simple"}, {IconTrash(), "trash"},
		{IconX(), "x"}, {IconCheck(), "check"}, {IconSettings(), "sliders-horizontal"}, {IconCalc(), "calculator"}, {IconInfo(), "info"},
	}
	for _, n := range named {
		want := `<i class="ph ph-` + n.name + ` icon" aria-hidden="true"></i>`
		if got := render(t, n.c); got != want {
			t.Errorf("%s icon = %q, want %q", n.name, got, want)
		}
	}
}

func TestLayoutWithoutActiveDebts(t *testing.T) {
	html := render(t, Layout(Shell{Active: "debts", DebtCount: 1, Here: "/debts"}, "Debts"))
	if strings.Contains(html, `href="/simulate"`) || strings.Contains(html, `class="nav-item" aria-current="false" href="/"`) {
		t.Error("plan and simulate should not be links without active debts")
	}
	for _, want := range []string{`title="Add a debt to see your plan"`, `title="Add a debt to try changes"`} {
		if !strings.Contains(tag(html, want), `aria-disabled="true"`) {
			t.Errorf("%s should be disabled: %q", want, tag(html, want))
		}
	}
	if !strings.Contains(tag(html, `href="/debts"`, "nav-item"), `aria-current="page"`) {
		t.Error("debts nav should still be a current link")
	}
}
