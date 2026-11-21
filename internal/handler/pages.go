package handler

import (
	"net/http"
	"strings"
)

func notice(r *http.Request) string {
	q := r.URL.Query()
	name := strings.TrimSpace(q.Get("name"))
	if name == "" {
		name = "Debt"
	}
	switch q.Get("msg") {
	case "added":
		return name + " added. Your plan has been updated."
	case "updated":
		return name + " updated. Your plan has been updated."
	case "deleted":
		return name + " deleted."
	case "activated":
		return name + " is back in your plan and Simulate. Months already marked since its start date count its minimum payment."
	case "deactivated":
		return name + " is now left out of your plan and Simulate."
	case "settings":
		return "Settings saved."
	}
	return ""
}
