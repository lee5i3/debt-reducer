// Package components holds the building blocks shared by every page:
// the app shell (sidebar and top bar), dialogs, alerts, icons and inputs.
package components

import (
	"strconv"
	"strings"
	"time"

	"github.com/lee5i3/debt-reducer/internal/debt"
)

// Shell is the data shown in the sidebar and top bar on every page.
type Shell struct {
	Active    string // "plan", "simulate", "debts" or "settings"
	DebtCount int
	TotalOwed debt.Money
	DebtFree  time.Time // zero when there is no plan
	// InterestPaid and InterestAhead are the plan's interest so far and to come.
	InterestPaid  debt.Money
	InterestAhead debt.Money
	Theme         debt.Theme
	// NoticeSeconds closes "saved" banners after this long (0 = never).
	NoticeSeconds int
	// Here is the current page's URL without the settings flag; the
	// settings popup opens over it and closes back to it.
	Here string
	// Settings holds the saved preferences when the settings popup is open.
	Settings *debt.Preferences
	// SettingsError is shown in the settings popup.
	SettingsError string
}

// SettingsURL opens the settings popup over the current page.
func (s Shell) SettingsURL() string {
	here := s.Here
	if here == "" {
		here = "/"
	}
	if strings.Contains(here, "?") {
		return here + "&settings=1"
	}
	return here + "?settings=1"
}

// CloseURL is where closing the settings popup goes.
func (s Shell) CloseURL() string {
	if s.Here == "" {
		return "/"
	}
	return s.Here
}

// ThemeName is the page's color scheme, dark unless set to light.
func (s Shell) ThemeName() string {
	if s.Theme == debt.ThemeLight {
		return string(debt.ThemeLight)
	}
	return string(debt.ThemeDark)
}

// noticeLabel is how an auto-close choice reads in Settings.
func noticeLabel(n int) string {
	if n == 0 {
		return "Never"
	}
	return strconv.Itoa(n) + " seconds"
}
