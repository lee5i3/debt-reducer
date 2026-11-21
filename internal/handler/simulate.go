package handler

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/lee5i3/debt-reducer/internal/debt"
	"github.com/lee5i3/debt-reducer/internal/service"
	"github.com/lee5i3/debt-reducer/web/views/pages"
)

// simMonths is how many months ahead a change can start.
const simMonths = 12

type simInput struct {
	mode   string // "one" or "monthly"
	raw    string
	amount debt.Money
	month  int
}

func readSimInput(get func(string) string) (simInput, error) {
	in := simInput{mode: get("mode"), raw: get("amount")}
	if in.mode != "monthly" {
		in.mode = "one"
	}
	if in.raw == "" {
		in.raw = "5,000.00"
		if in.mode == "monthly" {
			in.raw = "200.00"
		}
	}
	in.month, _ = strconv.Atoi(get("month"))
	in.month = max(0, min(simMonths-1, in.month))
	m, err := debt.ParseDollars(in.raw)
	if err != nil || m <= 0 {
		return in, errors.New("enter an amount above $0")
	}
	in.amount = m
	return in, nil
}

func (in simInput) extra(i int) debt.Money {
	if in.mode == "one" {
		if i == in.month {
			return in.amount
		}
		return 0
	}
	if i >= in.month {
		return in.amount
	}
	return 0
}

// SimulatePage compares the plan with a one-time payment or a bigger
// monthly payment.
func (h *Handler) SimulatePage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tp, err := h.plans.Tracked(ctx)
	if err != nil {
		h.errorPage(w, r, "simulate", err)
		return
	}
	in, inErr := readSimInput(r.URL.Query().Get)
	data := pages.SimData{
		Shell:   h.shellFrom(r, "simulate", tp),
		Set:     tp.Set,
		Mode:    in.mode,
		Amount:  in.raw,
		Month:   strconv.Itoa(in.month),
		ShowAll: r.URL.Query().Get("all") == "1",
	}
	if tp.Owed == 0 {
		data.Empty = true
		h.render(w, r, http.StatusOK, pages.SimulatePage(data))
		return
	}

	proj, err := h.plans.Simulate(ctx, in.extra)
	if err == nil && (proj.Base == nil || proj.Changed == nil) {
		err = debt.ErrNoDebts
	}
	for i := 0; i < simMonths; i++ {
		var m time.Time
		if proj != nil {
			m = proj.Next.AddDate(0, i, 0)
		}
		data.Months = append(data.Months, pages.Option{Value: strconv.Itoa(i), Label: m.Format("January 2006")})
	}
	switch {
	case inErr != nil:
		data.Error = inErr.Error()
	case err != nil:
		data.Error = h.userMessage(err)
	}
	if data.Error != "" {
		h.render(w, r, http.StatusOK, pages.SimulatePage(data))
		return
	}

	base, changed := proj.Base, proj.Changed
	monthAt := func(i int) time.Time { return proj.Next.AddDate(0, i, 0) }
	data.Base = pages.SimSummary{DebtFree: monthAt(base.Months - 1), Months: base.Months, Interest: base.TotalInterest}
	data.Changed = pages.SimSummary{DebtFree: monthAt(changed.Months - 1), Months: changed.Months, Interest: changed.TotalInterest}
	data.Saved = max(0, base.TotalInterest-changed.TotalInterest)
	data.Sooner = base.Months - changed.Months
	data.Total = len(changed.Schedule)
	if in.mode == "one" {
		data.Allocs = allocations(base, changed, in.month)
		data.AllocMonth = monthAt(in.month)
		data.AllocAmount = in.amount
	}

	all, _ := h.debts.List(ctx)
	target := extraTarget(base, changed, in.month, all, tp.Strategy, tp.Order)
	planned := tp.MonthlyBudget
	if in.month < len(base.Schedule) {
		planned = max(planned, base.Schedule[in.month].TotalPaid)
	}
	if in.mode == "one" {
		data.Note = fmt.Sprintf("The extra goes to %s first, then the next debt in your plan. Your regular %s payment still happens that month.", target, planned)
	} else {
		data.Note = fmt.Sprintf("You'd pay %s a month instead of %s from %s. The extra goes to the debt your plan is paying down first.", tp.MonthlyBudget+in.amount, tp.MonthlyBudget, monthAt(in.month).Format("January 2006"))
	}
	if in.mode == "one" {
		if res, err := h.planner.LumpSum(ctx, in.amount, service.Options{Strategy: tp.Strategy, MonthlyBudget: tp.MonthlyBudget}); err == nil && res.Analysis != nil {
			if best := res.Analysis.Best(); best != nil && best.Target != target {
				data.Tip = fmt.Sprintf("Tip: putting all of it on %s instead would save %s in interest.", best.Target, best.InterestSaved)
			}
		}
	}

	limit := 12
	if data.ShowAll {
		limit = len(changed.Schedule)
	}
	for i, m := range changed.Schedule {
		if i >= limit {
			break
		}
		row := pages.SimRow{Month: monthAt(i), Paid: m.TotalPaid, Interest: m.TotalInterest, Left: m.RemainingBalance}
		if i < len(base.Schedule) {
			row.BaseLeft = base.Schedule[i].RemainingBalance
		}
		if x := in.extra(i); x > 0 {
			row.Applies = true
			if in.mode == "one" {
				row.Note = "Includes " + x.String() + " one-time payment"
			} else {
				row.Note = "Includes " + x.String() + " extra"
			}
		} else if m.RemainingBalance == 0 {
			row.Note = "Debt free"
		}
		data.Rows = append(data.Rows, row)
	}
	h.render(w, r, http.StatusOK, pages.SimulatePage(data))
}

// extraTarget names the debt that the extra money reaches first in month i:
// of the debts paid more than in the current plan, the one the strategy
// puts first (highest rate, or smallest balance for snowball).
func extraTarget(base, changed *debt.Plan, i int, debts []debt.Debt, s debt.Strategy, o debt.Order) string {
	if i >= len(changed.Schedule) {
		return "the debt your plan pays first"
	}
	before := map[int64]debt.Money{}
	if i < len(base.Schedule) {
		for _, pay := range base.Schedule[i].Payments {
			before[pay.DebtID] = pay.Amount
		}
	}
	byID := map[int64]debt.Debt{}
	for _, d := range debts {
		byID[d.ID] = d
	}
	var best *debt.Debt
	for _, pay := range changed.Schedule[i].Payments {
		if pay.Amount <= before[pay.DebtID] {
			continue
		}
		d, ok := byID[pay.DebtID]
		if !ok {
			continue
		}
		if best == nil || comesFirst(d, *best, s, o) {
			best = &d
		}
	}
	if best == nil {
		return "the debt your plan pays first"
	}
	return best.Name
}

// ApplySimulation adds a simulated change to the saved plan as edited months.
func (h *Handler) ApplySimulation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_ = r.ParseForm()
	in, err := readSimInput(r.PostFormValue)
	if err != nil {
		h.errorPage(w, r, "simulate", fmt.Errorf("%w: %w", service.ErrInvalid, err))
		return
	}
	tp, err := h.plans.Tracked(ctx)
	if err == nil && !tp.Set {
		err = fmt.Errorf("%w: set up your plan first", service.ErrInvalid)
	}
	var proj *service.Projection
	if err == nil {
		proj, err = h.plans.Simulate(ctx, in.extra)
	}
	if err == nil && proj.Changed == nil {
		err = debt.ErrNoDebts
	}
	if err != nil {
		h.errorPage(w, r, "simulate", err)
		return
	}
	month := proj.Next.AddDate(0, in.month, 0)
	msg := "sim-one"
	if in.mode == "monthly" {
		msg = "sim-monthly"
		err = h.plans.RaiseMonthly(ctx, month, in.amount)
	} else {
		err = h.plans.AddExtra(ctx, month, in.amount)
	}
	if err != nil {
		h.errorPage(w, r, "simulate", err)
		return
	}
	redirectPlan(w, r, msg, month, in.amount.String())
}

// comesFirst reports whether the strategy gives extra money to a before b.
func comesFirst(a, b debt.Debt, s debt.Strategy, o debt.Order) bool {
	switch s {
	case debt.Snowball:
		return a.Balance < b.Balance
	case debt.Custom:
		o = o.Normalize()
		high := o.First == debt.OrderHighFirst
		if o.By == debt.OrderByBalance {
			return (a.Balance > b.Balance) == high && a.Balance != b.Balance
		}
		return (a.APR > b.APR) == high && a.APR != b.APR
	default:
		return a.APR > b.APR
	}
}

// allocations compares month i with and without the one-time payment: each
// account paid more is where the money went. Accounts it pays off come
// first (the money reaches them first), then the rest by amount.
func allocations(base, changed *debt.Plan, i int) []pages.SimAlloc {
	if i >= len(changed.Schedule) {
		return nil
	}
	before := map[int64]debt.Payment{}
	if i < len(base.Schedule) {
		for _, p := range base.Schedule[i].Payments {
			before[p.DebtID] = p
		}
	}
	var out []pages.SimAlloc
	for _, p := range changed.Schedule[i].Payments {
		b := before[p.DebtID]
		if extra := p.Amount - b.Amount; extra > 0 {
			out = append(out, pages.SimAlloc{Name: p.Debt, Amount: extra, PaysOff: p.PaidOff && !b.PaidOff, Left: p.Remaining})
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].PaysOff != out[b].PaysOff {
			return out[a].PaysOff
		}
		return out[a].Amount > out[b].Amount
	})
	return out
}
