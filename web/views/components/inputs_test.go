package components

import "testing"

func TestFormatMoney(t *testing.T) {
	cases := map[string]string{
		"":          "",
		"5000":      "5,000.00",
		"1234.5":    "1,234.50",
		"$1,409.38": "1,409.38",
		" 62.384 ":  "62.38",
		"abc":       "abc", // left for the user to fix
		"0":         "0.00",
	}
	for in, want := range cases {
		if got := FormatMoney(in); got != want {
			t.Errorf("FormatMoney(%q) = %q, want %q", in, got, want)
		}
	}
}
