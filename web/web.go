// Package web holds the UI: templ components live in web/views and static
// assets are embedded here.
package web

import "embed"

// Static holds CSS and other static assets served under /static/.
//
//go:embed static
var Static embed.FS
