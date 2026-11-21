package service

import (
	"time"

	"github.com/lee5i3/debt-reducer/internal/debt"
)

// replay works out what's left on each debt: it starts from the amount the
// debt was entered with (as of its start date) and applies every marked
// plan month from that date's month on, in order. Each applied month adds
// that month's interest on what was left and takes off what the month paid
// the debt. Promotional purchases are paid down earliest-expiring first.
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
		seen := map[int64]bool{}
		for _, row := range m.Debts {
			seen[row.DebtID] = true
			d, ok := left[row.DebtID]
			if !ok || m.Month.Before(FirstOfMonth(d.StartDate)) {
				// A deleted debt, or a month before its starting amount:
				// show what was recorded, change nothing.
				history[i] = append(history[i], row)
				continue
			}
			var r debt.MonthDebt
			left[d.ID], r = applyMonth(d, m.Status, row.Paid, row.Role)
			history[i] = append(history[i], r)
		}
		// An active debt the month didn't record (it was inactive then, or
		// added later with an earlier start date) still had its minimum due:
		// count it from the debt's start month on so its balance is right.
		for _, d0 := range debts {
			d := left[d0.ID]
			if seen[d.ID] || d.Inactive || d.Balance == 0 || m.Month.Before(FirstOfMonth(d.StartDate)) {
				continue
			}
			var r debt.MonthDebt
			left[d.ID], r = applyMonth(d, m.Status, d.MinimumPayment, debt.RoleMinimum)
			history[i] = append(history[i], r)
		}
	}
	return left, history
}

// applyMonth adds a month's interest to what's left on d and takes off the
// payment (nothing in a skipped month). It returns the updated debt and
// its row for the month.
func applyMonth(d debt.Debt, status string, pay debt.Money, role string) (debt.Debt, debt.MonthDebt) {
	before := d.Balance
	interest := debt.Money(0)
	if before > 0 {
		interest = debt.MonthlyInterest(before, d.APR)
	}
	paid := min(pay, before+interest)
	if status == debt.MonthSkipped {
		paid = 0
	}
	d.Balance = before + interest - paid
	payPromos(&d, paid-interest)

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
		Paid: paid, Minimum: min(d.MinimumPayment, paid), Left: d.Balance, Role: role,
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
