package handler

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/lee5i3/debt-reducer/internal/debt"
	"github.com/lee5i3/debt-reducer/internal/service"
	"github.com/lee5i3/debt-reducer/web/views"
)

// Dashboard renders the payoff plan for the saved debts.
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	opts, optErr := planOptions(r)
	if optErr != nil {
		opts = service.Options{} // fall back to saved settings
	}
	res, err := h.planner.Build(r.Context(), opts)
	if res == nil {
		h.render(w, r, statusFor(err), views.ErrorPage(h.userMessage(err)))
		return
	}

	data := views.IndexData{
		Debts:         res.Debts,
		MinimumTotal:  res.MinimumTotal,
		Strategy:      res.Options.Strategy,
		MonthlyBudget: res.Options.MonthlyBudget,
		StartDate:     res.Options.StartDate,
		Plan:          res.Plan,
		Comparison:    res.Comparison,
		Notice:        notice(r),
	}
	for _, d := range res.Debts {
		data.TotalBalance += d.Balance
	}
	for _, p := range res.Comparison {
		if p.Strategy == debt.MinimumOnly {
			data.Baseline = p
		}
	}
	switch {
	case optErr != nil:
		data.Error = optErr.Error()
	case err != nil:
		data.Error = h.userMessage(err)
	}
	h.render(w, r, http.StatusOK, views.Index(data))
}

// SaveSettings stores the dashboard's strategy and budget as defaults.
func (h *Handler) SaveSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	budget, err := debt.ParseDollars(r.PostFormValue("budget"))
	if err != nil {
		http.Redirect(w, r, "/?"+url.Values{"strategy": {r.PostFormValue("strategy")}}.Encode(), http.StatusSeeOther)
		return
	}
	s := debt.Settings{Strategy: debt.Strategy(r.PostFormValue("strategy")), MonthlyBudget: budget}
	if err := h.debts.SaveSettings(r.Context(), s); err != nil {
		h.render(w, r, statusFor(err), views.ErrorPage(h.userMessage(err)))
		return
	}
	q := url.Values{"msg": {"settings"}}
	if start := r.PostFormValue("start"); start != "" {
		q.Set("start", start)
	}
	http.Redirect(w, r, "/?"+q.Encode(), http.StatusSeeOther)
}

// DebtsPage lists debts with an empty "add" form.
func (h *Handler) DebtsPage(w http.ResponseWriter, r *http.Request) {
	h.renderDebts(w, r, http.StatusOK, views.DebtForm{DueDay: "1"})
}

// EditDebtPage lists debts with the selected debt loaded in the form.
func (h *Handler) EditDebtPage(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d, err := h.debts.Get(r.Context(), id)
	if err != nil {
		h.render(w, r, statusFor(err), views.ErrorPage(h.userMessage(err)))
		return
	}
	h.renderDebts(w, r, http.StatusOK, views.FormFromDebt(d))
}

// CreateDebt handles the add form.
func (h *Handler) CreateDebt(w http.ResponseWriter, r *http.Request) {
	form, d, err := parseDebtForm(r)
	if err == nil {
		_, err = h.debts.Create(r.Context(), d)
	}
	if err != nil {
		form.Error = h.userMessage(err)
		h.renderDebts(w, r, statusFor(err), form)
		return
	}
	http.Redirect(w, r, "/debts?msg=added", http.StatusSeeOther)
}

// UpdateDebt handles the edit form.
func (h *Handler) UpdateDebt(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	form, d, err := parseDebtForm(r)
	form.ID, d.ID = id, id
	if err == nil {
		err = h.debts.Update(r.Context(), d)
	}
	if err != nil {
		form.Error = h.userMessage(err)
		h.renderDebts(w, r, statusFor(err), form)
		return
	}
	http.Redirect(w, r, "/debts?msg=updated", http.StatusSeeOther)
}

// DeleteDebt removes a debt.
func (h *Handler) DeleteDebt(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := h.debts.Delete(r.Context(), id); err != nil && !errors.Is(err, service.ErrNotFound) {
		h.render(w, r, statusFor(err), views.ErrorPage(h.userMessage(err)))
		return
	}
	http.Redirect(w, r, "/debts?msg=deleted", http.StatusSeeOther)
}

func (h *Handler) renderDebts(w http.ResponseWriter, r *http.Request, status int, form views.DebtForm) {
	debts, err := h.debts.List(r.Context())
	if err != nil {
		h.render(w, r, statusFor(err), views.ErrorPage(h.userMessage(err)))
		return
	}
	data := views.DebtsData{Debts: debts, Form: form, MinimumTotal: debt.MinimumTotal(debts), Notice: notice(r)}
	for _, d := range debts {
		data.TotalBalance += d.Balance
	}
	h.render(w, r, status, views.DebtsPage(data))
}

// parseDebtForm returns the raw form (for re-rendering) and the parsed debt.
func parseDebtForm(r *http.Request) (views.DebtForm, debt.Debt, error) {
	if err := r.ParseForm(); err != nil {
		return views.DebtForm{}, debt.Debt{}, fmt.Errorf("%w: %w", service.ErrInvalid, err)
	}
	f := views.DebtForm{
		Name:           strings.TrimSpace(r.PostFormValue("name")),
		Balance:        r.PostFormValue("balance"),
		APR:            r.PostFormValue("apr"),
		MinimumPayment: r.PostFormValue("minimum_payment"),
		DueDay:         r.PostFormValue("due_day"),
	}
	d := debt.Debt{Name: f.Name}
	var errs []error
	var err error
	if d.Balance, err = debt.ParseDollars(f.Balance); err != nil {
		errs = append(errs, fmt.Errorf("balance: %w", err))
	}
	if d.MinimumPayment, err = debt.ParseDollars(f.MinimumPayment); err != nil {
		errs = append(errs, fmt.Errorf("minimum payment: %w", err))
	}
	if d.APR, err = strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(f.APR), "%"), 64); err != nil {
		errs = append(errs, errors.New("apr must be a number"))
	}
	if d.DueDay, err = strconv.Atoi(strings.TrimSpace(f.DueDay)); err != nil {
		errs = append(errs, errors.New("due day must be a whole number"))
	}
	if len(errs) > 0 {
		return f, d, fmt.Errorf("%w: %w", service.ErrInvalid, errors.Join(errs...))
	}
	return f, d, nil
}

func notice(r *http.Request) string {
	switch r.URL.Query().Get("msg") {
	case "added":
		return "Debt added."
	case "updated":
		return "Debt updated."
	case "deleted":
		return "Debt deleted."
	case "settings":
		return "Defaults saved."
	}
	return ""
}
