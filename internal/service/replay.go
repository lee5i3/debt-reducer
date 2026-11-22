package service

import (
	"time"

	"github.com/lee5i3/debt-reducer/internal/debt"
)

// replay works out what's left on each debt: it starts from the amount the
// debt was entered with (as of its start date) and applies every marked
// plan month from that date's month on, in order, using the payment,
// interest and minimum recorded when the month was marked. Marked months
// are history: a debt that wasn't in one (inactive then, or added later)
// is never added to it, so a reactivated debt picks up from the next
// unmarked month with the amount it had. Promotional purchases are paid
// down earliest-expiring first.
//
// It returns each debt with Balance (and Promos) set to what's left, and
// the per-debt rows for each marked month, recomputed the same way so
// history stays consistent if a starting amount is corrected later.
func replay(debts []debt.Debt, marked []debt.MonthRecord) (map[int64]debt.Debt, [][]debt.MonthDebt) {
	left := make(map[int64]debt.Debt, len(debts))
	for _, d := range debts {
		d.Promos = append([]debt.PromoItem(nil), d.Promos...)
		left[d.ID] = d
	}

	history := make([][]debt.MonthDebt, len(marked))
	for i, m := range marked {
		for _, row := range m.Debts {
			d, ok := left[row.DebtID]
			if !ok || m.Month.Before(FirstOfMonth(d.StartDate)) {
				// A deleted debt, or a month before its starting amount:
				// show what was recorded, change nothing.
				history[i] = append(history[i], row)
				continue
			}
			var r debt.MonthDebt
			left[d.ID], r = applyRecorded(d, m.Status, row)
			history[i] = append(history[i], r)
		}
	}
	return left, history
}

// applyRecorded applies a month as it was marked: the payment, interest
// and minimum recorded then stay as they are, even if the debt's minimum or
// APR is edited later. Only the balances follow the debt's starting amount.
func applyRecorded(d debt.Debt, status string, row debt.MonthDebt) (debt.Debt, debt.MonthDebt) {
	before := d.Balance
	interest, paid := row.Interest, row.Paid
	if status == debt.MonthSkipped {
		paid = 0
	}
	d.Balance = max(before+interest-paid, 0)
	payPromos(&d, paid-interest)

	role := row.Role
	switch {
	case status == debt.MonthSkipped:
		role = debt.RoleSkipped
	case d.Balance == 0 && before > 0:
		role = debt.RolePaidOff
	case role == debt.RolePaidOff:
		role = debt.RoleExtra // no longer clears it (starting amount changed)
	}
	return d, debt.MonthDebt{
		DebtID: d.ID, Name: d.Name, Before: before, Interest: interest,
		Paid: paid, Minimum: min(row.Minimum, paid), Left: d.Balance, Role: role,
	}
}

// payPromos takes principal off a promotional account's purchases, earliest
// expiring first (they're stored in that order). Negative principal (interest
// added) goes on the last purchase.
func payPromos(d *debt.Debt, principal debt.Money) {
	if len(d.Promos) == 0 || principal == 0 {
		return
	}
	if principal < 0 {
		d.Promos[len(d.Promos)-1].Remaining -= principal
		return
	}
	for i := range d.Promos {
		take := min(d.Promos[i].Remaining, principal)
		d.Promos[i].Remaining -= take
		principal -= take
		if principal == 0 {
			return
		}
	}
}

// startOrToday is a debt's start date, or today if it has none.
func startOrToday(d debt.Debt, today time.Time) time.Time {
	if d.StartDate.IsZero() {
		return today
	}
	return d.StartDate
}
