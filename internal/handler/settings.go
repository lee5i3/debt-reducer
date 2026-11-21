package handler

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/lee5i3/debt-reducer/internal/debt"
)

// SettingsPage opens the settings popup over the plan.
func (h *Handler) SettingsPage(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/?settings=1", http.StatusSeeOther)
}

// SaveSettings stores the display preferences from the settings popup and
// goes back to the page it was opened on.
func (h *Handler) SaveSettings(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	p := debt.Preferences{
		Theme:         debt.Theme(r.PostFormValue("theme")),
		ExpandCurrent: r.PostFormValue("expand_current") == "1",
		NoticeSeconds: -1, // invalid unless the form sends a number
	}
	if n, err := strconv.Atoi(r.PostFormValue("notice_seconds")); err == nil {
		p.NoticeSeconds = n
	}
	back := localURL(r.PostFormValue("back"))
	if err := h.debts.SavePreferences(r.Context(), p); err != nil {
		u, _ := url.Parse(back)
		q := u.Query()
		q.Set("settings", "1")
		q.Set("settings_err", "1")
		u.RawQuery = q.Encode()
		http.Redirect(w, r, u.String(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// localURL keeps a redirect target on this site: a path starting with one
// "/". Anything else goes to the plan.
func localURL(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "" || u.Host != "" || !strings.HasPrefix(u.Path, "/") ||
		strings.HasPrefix(s, "//") || strings.HasPrefix(s, "/\\") {
		return "/"
	}
	return u.RequestURI()
}

// APIGetPreferences handles GET /api/preferences.
func (h *Handler) APIGetPreferences(w http.ResponseWriter, r *http.Request) {
	p, err := h.debts.Preferences(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// APISavePreferences handles PUT /api/preferences.
func (h *Handler) APISavePreferences(w http.ResponseWriter, r *http.Request) {
	var p debt.Preferences
	if err := decode(r, &p); err != nil {
		h.writeError(w, err)
		return
	}
	if err := h.debts.SavePreferences(r.Context(), p); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}
