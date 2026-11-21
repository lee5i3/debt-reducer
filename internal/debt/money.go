package debt

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Money is an amount in cents. Using integer cents avoids floating point
// drift when running long amortization schedules.
type Money int64

// FromDollars converts a dollar amount (e.g. 1234.56) to Money.
func FromDollars(d float64) Money {
	return Money(math.Round(d * 100))
}

// Dollars returns the amount as a float dollar value.
func (m Money) Dollars() float64 {
	return float64(m) / 100
}

// String formats the amount as "$1,234.56".
func (m Money) String() string {
	sign := ""
	v := int64(m)
	if v < 0 {
		sign = "-"
		v = -v
	}
	whole := strconv.FormatInt(v/100, 10)
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return fmt.Sprintf("%s$%s.%02d", sign, b.String(), v%100)
}

// MarshalJSON renders Money as a dollar number in JSON.
func (m Money) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatFloat(m.Dollars(), 'f', 2, 64)), nil
}

// UnmarshalJSON accepts a dollar number, e.g. 1234.56.
func (m *Money) UnmarshalJSON(b []byte) error {
	f, err := strconv.ParseFloat(string(b), 64)
	if err != nil {
		return fmt.Errorf("money must be a number of dollars: %w", err)
	}
	*m = FromDollars(f)
	return nil
}

// ParseDollars parses user input such as "1,234.56" or "$99".
func ParseDollars(s string) (Money, error) {
	s = strings.NewReplacer("$", "", ",", "", " ", "").Replace(s)
	if s == "" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("%q is not a valid amount", s)
	}
	return FromDollars(f), nil
}

func minMoney(a, b Money) Money {
	if a < b {
		return a
	}
	return b
}
