package debt

import "math"

// MinimumFloor is the smallest calculated minimum payment for cards and
// promotional purchases.
const MinimumFloor Money = 2500

// Terms is what the minimum payment of a debt is worked out from.
type Terms struct {
	Type         string
	Balance      Money
	APR          float64 // ignored while InterestFree
	Months       int     // loan: months remaining; promotion: interest-free months left
	InterestFree bool    // a promotional purchase still in its no-interest period
	// Parts are a promotional account's purchases: each must be cleared in
	// its own number of payments. When set, they replace Balance/Months.
	Parts []PromoPart
}

// PromoPart is one promotional purchase for the minimum calculation.
type PromoPart struct {
	Remaining Money
	Payments  int // payments left before it expires
}

// Minimum returns the calculated monthly minimum payment:
//
//   - Loan: the fixed payment that pays it off over the months remaining.
//   - Promotional purchase, interest-free: clears the balance before the
//     promotion ends, at least MinimumFloor.
//   - Credit card (or a promotion that now charges interest): 1% of the
//     balance plus this month's interest, at least MinimumFloor.
//
// It never asks for more than the balance plus one month's interest.
func (t Terms) Minimum() Money {
	bal := float64(t.Balance)
	r := t.APR / 1200
	if t.InterestFree {
		r = 0
	}
	var v float64
	switch {
	case t.Type == TypeLoan && t.Months > 0:
		if r == 0 {
			v = bal / float64(t.Months)
		} else {
			v = bal * r / (1 - math.Pow(1+r, -float64(t.Months)))
		}
	case t.Type == TypePromo && len(t.Parts) > 0:
		for _, p := range t.Parts {
			v += float64(p.Remaining) / float64(max(p.Payments, 1))
		}
		v = math.Max(float64(MinimumFloor), v)
	case t.Type == TypePromo && t.InterestFree && t.Months > 0:
		v = math.Max(float64(MinimumFloor), bal/float64(t.Months))
	default:
		v = math.Max(float64(MinimumFloor), 0.01*bal+bal*r)
	}
	v = math.Min(v, bal*(1+r))
	return Money(math.Round(v))
}
