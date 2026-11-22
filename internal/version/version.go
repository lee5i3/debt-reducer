// Package version holds the app's name and version. The build sets Version
// from the git tag, or the next version CI works out (see the Makefile);
// `go run` and tests show "dev".
package version

import "strings"

// Name is the app's display name.
const Name = "Debt Reducer"

// Version is set at build time with
// -ldflags "-X github.com/lee5i3/debt-reducer/internal/version.Version=1.2.3".
var Version = "dev"

// String is the name and version as shown in the app, e.g. "Debt Reducer 0.1.0".
func String() string {
	if Version == "dev" || strings.HasSuffix(Version, "-dev") {
		return Name + " (dev build)"
	}
	return Name + " " + Version
}
