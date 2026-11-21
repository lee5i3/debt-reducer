package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/lee5i3/debt-reducer/internal/debt"
	"github.com/lee5i3/debt-reducer/internal/service"
	"github.com/lee5i3/debt-reducer/web/views/pages"
)

// rowsShown is how many upcoming months the plan shows before "Show all".
const rowsShown = 12

// PlanPage shows the saved plan, or the setup form before there is one.
func (h *Handler) PlanPage(w http.ResponseWriter, r *http.Request) {
	h.renderPlan(w, r, http.StatusOK, pages.PlanData{})
}

// PlanSetupPage shows the plan with the setup dialog open.
func (h *Handler) PlanSetupPage(w http.ResponseWriter, r *http.Request) {
	h.renderPlan(w, r, http.StatusOK, pages.PlanData{Dialog: "setup"})
}

// SavePlanSetup handles the setup form.
func (h *Handler) SavePlanSetup(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	form := pages.SetupForm{
		Strategy: debt.Strategy(r.PostFormValue("strategy")),
		Order:    debt.Order{By: r.PostFormValue("order_by"), First: r.PostFormValue("order_first")}.Normalize(),
		Pay:      r.PostFormValue("pay"),
		Extra:    strings.TrimSpace(r.PostFormValue("extra")),
		Start:    r.PostFormValue("start"),
	}
	// The plan pays whatever the minimums add up to, plus the extra.
	var extra debt.Money
	var err error
	if form.Pay == "extra" {
		var perr error
		extra, perr = debt.ParseDollars(form.Extra)
		if perr != nil || extra <= 0 {
			err = fmt.Errorf("%w: enter the extra as a number above 0, or choose Minimum", service.ErrInvalid)
		}
	} else {
		form.Pay, form.Extra = "min", ""
	}
	var start time.Time
	if err == nil {
		start, err = time.ParseInLocation("2006-01", form.Start, time.Local)
		if err != nil {
			err = fmt.Errorf("%w: pick the first month of your plan", service.ErrInvalid)
		}
	}
	if err == nil {
		err = h.plans.Setup(r.Context(), form.Strategy, form.Order, 0, extra, start)
	}
	if err != nil {
		form.Error = h.userMessage(err)
		tp, _ := h.plans.Tracked(r.Context())
		dialog := ""
		if tp != nil && tp.Set {
			dialog = "setup"
		}
		h.renderPlan(w, r, http.StatusBadRequest, pages.PlanData{Dialog: dialog, Setup: form})
		return
	}
	http.Redirect(w, r, "/?msg=plan", http.StatusSeeOther)
}

// MarkMonth handles Paid, Skip and Undo from a month's menu.
func (h *Handler) MarkMonth(w http.ResponseWriter, r *http.Request) {
	month, ok := monthParam(w, r)
	if !ok {
		return
	}
	action := chi.URLParam(r, "action")
	var err error
	switch action {
	case "paid":
		err = h.plans.Mark(r.Context(), month, debt.MonthPaid)
	case "skip":
		err = h.plans.Mark(r.Context(), month, debt.MonthSkipped)
	case "undo":
		err = h.plans.Undo(r.Context(), month)
	case "remove-extra":
		err = h.plans.RemoveExtra(r.Context(), month)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.renderPlan(w, r, statusFor(err), pages.PlanData{Error: h.userMessage(err)})
		return
	}
	redirectPlan(w, r, action, month, "")
}

// EditMonthPage shows the plan with a month's edit-payment dialog open.
func (h *Handler) EditMonthPage(w http.ResponseWriter, r *http.Request) {
	month, ok := monthParam(w, r)
	if !ok {
		return
	}
	edit, err := h.editForm(r, month)
	if err != nil {
		h.errorPage(w, r, "plan", err)
		return
	}
	h.renderPlan(w, r, http.StatusOK, pages.PlanData{Dialog: "edit", Edit: edit})
}

// editForm builds the dialog rows from the month's planned payments: each
// account at its minimum, or minimum plus the extra the plan gives it.
func (h *Handler) editForm(r *http.Request, month time.Time) (pages.EditForm, error) {
	form := pages.EditForm{Month: month}
	tp, err := h.plans.Tracked(r.Context())
	if err != nil {
		return form, err
	}
	debts, err := h.debts.List(r.Context())
	if err != nil {
		return form, err
	}
	mins := map[int64]debt.Money{}
	for _, d := range debts {
		mins[d.ID] = d.MinimumPayment
	}
	for _, m := range tp.Months {
		if !m.Month.Equal(month) || !m.Upcoming() {
			continue
		}
		form.Planned, form.Edited, form.Extra = m.Paid, m.Edited, m.Extra
		if form.Plan, err = h.plans.PlanFor(r.Context(), month); err != nil {
			return form, err
		}
		for _, md := range m.Debts {
			row := pages.EditRow{DebtID: md.DebtID, Name: md.Name, Minimum: min(mins[md.DebtID], md.Before+md.Interest)}
			if paid := md.Paid; paid > row.Minimum && md.Role != debt.RolePaidOff || md.Role == debt.RoleExtra {
				row.WithExtra = true
				row.Extra = dollarsText(max(md.Paid-row.Minimum, 0))
			}
			form.Rows = append(form.Rows, row)
		}
		return form, nil
	}
	return form, fmt.Errorf("%w: %s isn't an upcoming month in your plan", service.ErrNotFound, month.Format("January 2006"))
}

// SaveMonthEdit sets what each account gets in a month, or resets it.
func (h *Handler) SaveMonthEdit(w http.ResponseWriter, r *http.Request) {
	month, ok := monthParam(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	if r.PostFormValue("reset") == "1" {
		err := h.plans.SetOverride(r.Context(), month, nil)
		if err == nil {
			err = h.plans.SetAllocations(r.Context(), month, nil)
		}
		if err != nil {
			h.renderPlan(w, r, statusFor(err), pages.PlanData{Error: h.userMessage(err)})
			return
		}
		redirectPlan(w, r, "reset", month, "")
		return
	}

	form, err := h.editForm(r, month)
	if err != nil {
		h.errorPage(w, r, "plan", err)
		return
	}
	amounts := map[int64]debt.Money{}
	bad := false
	for i := range form.Rows {
		row := &form.Rows[i]
		id := strconv.FormatInt(row.DebtID, 10)
		row.WithExtra = r.PostFormValue("choice_"+id) == "extra"
		row.Extra = strings.TrimSpace(r.PostFormValue("extra_" + id))
		amounts[row.DebtID] = row.Minimum
		if !row.WithExtra {
			row.Extra = ""
			continue
		}
		x, err := debt.ParseDollars(row.Extra)
		if err != nil || x <= 0 {
			row.Error = "Enter the extra as a number above 0, or choose Minimum."
			bad = true
			continue
		}
		amounts[row.DebtID] = row.Minimum + x
	}
	if !bad {
		err = h.plans.SetAllocations(r.Context(), month, amounts)
	}
	if bad || err != nil {
		if err != nil {
			form.Error = h.userMessage(err)
		}
		h.renderPlan(w, r, http.StatusBadRequest, pages.PlanData{Dialog: "edit", Edit: form})
		return
	}
	redirectPlan(w, r, "edited", month, form.Total().String())
}

func (h *Handler) renderPlan(w http.ResponseWriter, r *http.Request, status int, data pages.PlanData) {
	tp, err := h.plans.Tracked(r.Context())
	if err != nil {
		h.errorPage(w, r, "plan", err)
		return
	}
	data.Shell = h.shellFrom(r, "plan", tp)
	data.ExpandCurrent = h.preferences(r).ExpandCurrent
	data.Plan = tp
	data.ShowAll = r.URL.Query().Get("all") == "1"
	data.Rows, data.Hidden = visibleMonths(tp.Months, data.ShowAll)
	if data.Notice == "" {
		data.Notice = planNotice(r)
	}

	// Fill the setup form from the saved plan unless it holds submitted values.
	if data.Setup.Start == "" && data.Setup.Pay == "" {
		data.Setup.Strategy = tp.Strategy
		data.Setup.Order = tp.Order
		data.Setup.Pay = "min"
		if x := tp.MonthlyBudget - tp.MinimumTotal; x > 0 {
			data.Setup.Pay = "extra"
			data.Setup.Extra = dollarsText(x)
		} else if tp.FixedTotal && x < 0 {
			data.Setup.Note = fmt.Sprintf("Your plan is set to %s a month, which is less than your minimums (%s). Save to switch to Minimum or Min + extra.", tp.MonthlyBudget, tp.MinimumTotal)
		}
		data.Setup.Start = pages.MonthParam(tp.Start)
	}
	data.Setup.MinTotal = tp.MinimumTotal
	data.Setup.Locked = tp.Set && tp.MarkedCount > 0
	data.Setup.Months = monthOptions(tp.Start)
	if data.Dialog == "edit" {
		for _, m := range tp.Months {
			if m.Month.Equal(data.Edit.Month) {
				data.Edit.Edited, data.Edit.Extra = m.Edited, m.Extra
			}
		}
	}
	h.render(w, r, status, pages.PlanPage(data))
}

// visibleMonths returns every marked month (the page hides them until
// "Show hidden" is ticked) and the next rowsShown upcoming months.
func visibleMonths(months []service.PlanMonth, all bool) ([]service.PlanMonth, int) {
	if all {
		return months, 0
	}
	marked := 0
	for i, m := range months {
		if !m.Upcoming() {
			marked = i + 1
		}
	}
	last := min(len(months), marked+rowsShown)
	return months[:last], len(months) - last
}

// monthOptions offers the six months before today through three after,
// plus the saved start month if it falls outside that range.
func monthOptions(saved time.Time) []pages.Option {
	now := service.FirstOfMonth(time.Now())
	from := now.AddDate(0, -6, 0)
	if saved.Before(from) {
		from = saved
	}
	var opts []pages.Option
	for m := from; !m.After(now.AddDate(0, 3, 0)); m = m.AddDate(0, 1, 0) {
		opts = append(opts, pages.Option{Value: pages.MonthParam(m), Label: m.Format("January 2006")})
	}
	return opts
}

func monthParam(w http.ResponseWriter, r *http.Request) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01", chi.URLParam(r, "month"), time.Local)
	if err != nil {
		http.NotFound(w, r)
		return t, false
	}
	return t, true
}

func redirectPlan(w http.ResponseWriter, r *http.Request, msg string, month time.Time, amount string) {
	q := url.Values{"msg": {msg}, "m": {pages.MonthParam(month)}}
	if amount != "" {
		q.Set("amount", amount)
	}
	http.Redirect(w, r, "/?"+q.Encode(), http.StatusSeeOther)
}

func planNotice(r *http.Request) string {
	q := r.URL.Query()
	label := ""
	if t, err := time.ParseInLocation("2006-01", q.Get("m"), time.Local); err == nil {
		label = t.Format("January 2006")
	}
	switch q.Get("msg") {
	case "paid":
		return label + " marked paid. It now counts toward what you owe."
	case "skip":
		return label + " skipped. Interest added and the plan moved back a month."
	case "undo":
		return label + " set back to upcoming."
	case "edited":
		return label + " payment changed to " + q.Get("amount") + ". The rest of the plan was updated."
	case "reset":
		return label + " payment reset to the plan."
	case "plan":
		return "Plan saved."
	case "sim-one":
		return q.Get("amount") + " one-time payment added to " + label + "."
	case "sim-monthly":
		return "Monthly payment raised by " + q.Get("amount") + " from " + label + ". Your plan setup was updated."
	case "remove-extra":
		return "One-time payment removed from " + label + "."
	}
	return ""
}

// dollarsText formats money for an input box, e.g. "1,200.00".
func dollarsText(m debt.Money) string {
	s := m.String()
	if len(s) > 0 && s[0] == '$' {
		return s[1:]
	}
	return s
}
