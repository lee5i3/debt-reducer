package version

import "testing"

func TestString(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })

	Version = "dev"
	if got := String(); got != "Debt Reducer (dev build)" {
		t.Errorf("dev: %q", got)
	}
	Version = "1.2.3"
	if got := String(); got != "Debt Reducer 1.2.3" {
		t.Errorf("release: %q", got)
	}
}
