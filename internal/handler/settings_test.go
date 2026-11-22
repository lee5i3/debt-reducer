package handler_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/lee5i3/debt-reducer/internal/debt"
)

func (e *env) prefs() debt.Preferences {
	e.t.Helper()
	p, err := e.debts.Preferences(e.ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	return p
}

func TestSettingsPageRedirects(t *testing.T) {
	e := newEnv(t)
	u := wantRedirect(t, e.get("/settings"))
	if u.String() != "/?settings=1" {
		t.Fatalf("Location = %s, want /?settings=1", u)
	}
}

func TestSettingsDialogOnAnyPage(t *testing.T) {
	e := newEnv(t)
	rec := e.get("/debts?q=visa&settings=1&msg=added")
	wantStatus(t, rec, http.StatusOK)
	// The dialog posts back to the page it was opened on, minus the one-time bits.
	wantBody(t, rec, `id="settings-title"`, `action="/settings"`, `name="back" value="/debts?q=visa"`)

	rec = e.get("/debts")
	wantNoBody(t, rec, `id="settings-title"`)

	rec = e.get("/debts?settings=1&settings_err=1")
	wantBody(t, rec, "Those settings couldn", `name="back" value="/debts"`)

	// Other pages open it too.
	wantBody(t, e.get("/?settings=1"), `id="settings-title"`, `name="back" value="/"`)
	wantBody(t, e.get("/simulate?settings=1"), `id="settings-title"`, `name="back" value="/simulate"`)
}

func TestSaveSettings(t *testing.T) {
	e := newEnv(t)
	form := url.Values{"theme": {"light"}, "expand_current": {"1"}, "notice_seconds": {"10"}, "back": {"/debts?q=visa"}}
	u := wantRedirect(t, e.postForm("/settings", form))
	if u.String() != "/debts?q=visa" {
		t.Fatalf("Location = %s, want /debts?q=visa", u)
	}
	if p := e.prefs(); p.Theme != debt.ThemeLight || !p.ExpandCurrent || p.NoticeSeconds != 10 {
		t.Fatalf("saved prefs = %+v", p)
	}
	rec := e.get("/debts")
	wantBody(t, rec, `data-theme="light"`, `data-notice-seconds="10"`)

	// Unchecked box turns expand_current off.
	form = url.Values{"theme": {"dark"}, "notice_seconds": {"0"}, "back": {"/"}}
	wantRedirect(t, e.postForm("/settings", form))
	if p := e.prefs(); p.Theme != debt.ThemeDark || p.ExpandCurrent || p.NoticeSeconds != 0 {
		t.Fatalf("saved prefs = %+v", p)
	}
}

func TestSaveSettingsOpenRedirect(t *testing.T) {
	for _, back := range []string{"//evil.com", "https://evil.com/x", `/\evil.com`, "javascript:alert(1)", "", "relative/path"} {
		t.Run(back, func(t *testing.T) {
			e := newEnv(t)
			form := url.Values{"theme": {"dark"}, "notice_seconds": {"5"}, "back": {back}}
			u := wantRedirect(t, e.postForm("/settings", form))
			if u.String() != "/" {
				t.Fatalf("back=%q redirected to %q, want /", back, u)
			}
		})
	}
}

func TestSaveSettingsInvalid(t *testing.T) {
	tests := []struct {
		name string
		form url.Values
		want string
	}{
		{"bad theme", url.Values{"theme": {"pink"}, "notice_seconds": {"5"}, "back": {"/debts"}}, "/debts?settings=1&settings_err=1"},
		{"notice too long", url.Values{"theme": {"dark"}, "notice_seconds": {"99"}, "back": {"/debts?q=a"}}, "/debts?q=a&settings=1&settings_err=1"},
		{"notice missing", url.Values{"theme": {"dark"}, "back": {"/"}}, "/?settings=1&settings_err=1"},
		{"evil back", url.Values{"theme": {"pink"}, "back": {"//evil.com"}}, "/?settings=1&settings_err=1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			u := wantRedirect(t, e.postForm("/settings", tc.form))
			if u.String() != tc.want {
				t.Fatalf("Location = %s, want %s", u, tc.want)
			}
			if p := e.prefs(); p != debt.DefaultPreferences {
				t.Fatalf("invalid settings changed prefs: %+v", p)
			}
		})
	}
}

func TestNoticeBanner(t *testing.T) {
	e := newEnv(t)
	rec := e.get("/debts?msg=added&name=Visa")
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec,
		`<body data-notice-seconds="5">`,
		`class="notice" role="status" data-autoclose`,
		"Visa added. Your plan has been updated.",
		`class="banner-close" data-dismiss aria-label="Close message"`,
	)

	// Without a name the notice says "Debt".
	wantBody(t, e.get("/debts?msg=updated"), "Debt updated.")
	wantBody(t, e.get("/debts?msg=settings"), "Settings saved.")

	// Unknown messages show no banner.
	rec = e.get("/debts?msg=whatever")
	wantNoBody(t, rec, `role="status" data-autoclose`)
}

func TestPlanNoticeBanners(t *testing.T) {
	e := newEnv(t)
	m := monthParam(thisMonth())
	label := thisMonth().Format("January 2006")
	tests := map[string]string{
		"/?msg=undo&m=" + m:                        label + " set back to upcoming.",
		"/?msg=reset&m=" + m:                       label + " payment reset to the plan.",
		"/?msg=sim-one&amount=%24100.00&m=" + m:    "$100.00 one-time payment added to " + label + ".",
		"/?msg=sim-monthly&amount=%2450.00&m=" + m: "Monthly payment raised by $50.00 from " + label + ".",
		"/?msg=remove-extra&m=" + m:                "One-time payment removed from " + label + ".",
		"/?msg=edited&amount=%24300.00&m=" + m:     label + " payment changed to $300.00.",
		"/?msg=skip&m=" + m:                        label + " skipped.",
		"/?msg=paid&m=" + m:                        label + " marked paid.",
	}
	for path, want := range tests {
		t.Run(path, func(t *testing.T) {
			rec := e.get(path)
			wantStatus(t, rec, http.StatusOK)
			wantBody(t, rec, want, `role="status" data-autoclose`)
		})
	}
}

func TestHealthAndStatic(t *testing.T) {
	e := newEnv(t)
	rec := e.get("/healthz")
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[map[string]string](t, rec); got["status"] != "ok" {
		t.Fatalf("healthz = %v", got)
	}
	wantStatus(t, e.get("/favicon.ico"), http.StatusNoContent)
	wantStatus(t, e.get("/static/app.css"), http.StatusOK)
}
