package handler

import (
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lee5i3/debt-reducer/internal/debt"
	"github.com/lee5i3/debt-reducer/internal/service"
	"github.com/lee5i3/debt-reducer/web/views/pages"
)

// DebtsPage lists debts as cards.
func (h *Handler) DebtsPage(w http.ResponseWriter, r *http.Request) {
	h.renderDebts(w, r, http.StatusOK, pages.DebtsData{})
}

// NewDebtPage shows the list with the "Add a debt" dialog open.
func (h *Handler) NewDebtPage(w http.ResponseWriter, r *http.Request) {
	form := pages.DebtForm{Type: debt.DefaultType, Purchases: []pages.PurchaseRow{{}}, StartDate: today().Format(time.DateOnly), Active: true}
	h.renderDebts(w, r, http.StatusOK, pages.DebtsData{Dialog: "form", Form: form})
}

// DebtDetailPage shows the list with one debt's details dialog open.
func (h *Handler) DebtDetailPage(w http.ResponseWriter, r *http.Request) {
	d, ok := h.loadDebt(w, r)
	if !ok {
		return
	}
	left, err := h.plans.Remaining(r.Context())
	if err != nil {
		h.errorPage(w, r, "debts", err)
		return
	}
	var total debt.Money // what the plan covers: active debts only
	for _, x := range left {
		if !x.Inactive {
			total += x.Balance
		}
	}
	started := d
	if l, ok := left[d.ID]; ok {
		d = l // what's left, worked out from the plan
	}
	detail := pages.DebtDetail{
		Debt:              d,
		Started:           started,
		InterestThisMonth: debt.MonthlyInterest(d.Balance, d.APR),
	}
	if total > 0 {
		detail.SharePercent = int(math.Round(float64(d.Balance) / float64(total) * 100))
	}
	detail.PayoffMonths, detail.PayoffOK = h.planner.PayoffAtMinimum(d)
	if tp, err := h.plans.Tracked(r.Context()); err == nil && tp.Set {
		detail.PlanSet = true
		payments := 0
		for _, m := range tp.Months {
			if !m.Upcoming() {
				continue
			}
			payments++
			for _, md := range m.Debts {
				if md.DebtID == d.ID && md.Role == debt.RolePaidOff {
					detail.PlanPayoff, detail.PlanPayments = m.Month, payments
				}
			}
			if !detail.PlanPayoff.IsZero() {
				break
			}
		}
	}
	h.renderDebts(w, r, http.StatusOK, pages.DebtsData{Dialog: "detail", Detail: detail})
}

// EditDebtPage shows the list with the edit dialog open.
func (h *Handler) EditDebtPage(w http.ResponseWriter, r *http.Request) {
	d, ok := h.loadDebt(w, r)
	if !ok {
		return
	}
	form := pages.FormFromDebt(d, today())
	h.renderDebts(w, r, http.StatusOK, pages.DebtsData{Dialog: "form", Form: form})
}

// DeleteDebtPage shows the list with the delete confirmation open.
func (h *Handler) DeleteDebtPage(w http.ResponseWriter, r *http.Request) {
	d, ok := h.loadDebt(w, r)
	if !ok {
		return
	}
	h.renderDebts(w, r, http.StatusOK, pages.DebtsData{Dialog: "delete", Target: d})
}

// calculatePayment answers the loan "calculate" button when JavaScript is
// off: it re-shows the form with the payment filled in, without saving.
func (h *Handler) calculatePayment(w http.ResponseWriter, r *http.Request, id int64) bool {
	if r.PostFormValue("add_purchase") == "1" {
		f := formFromValues(r.PostForm)
		f.ID = id
		f.Purchases = append(f.Purchases, pages.PurchaseRow{})
		purchasesFrom(&f)
		f.Errors = map[string]string{}
		h.renderDebts(w, r, http.StatusOK, pages.DebtsData{Dialog: "form", Form: f})
		return true
	}
	if r.PostFormValue("calculate") != "1" {
		return false
	}
	f := formFromValues(r.PostForm)
	f.ID = id
	t, ok := termsFrom(&f)
	switch {
	case ok && t.Type == debt.TypeLoan && t.Months == 0:
		f.Errors["term_months"] = "Enter months remaining to calculate the payment."
	case ok:
		f.MinimumPayment = dollarsText(t.Minimum())
	}
	h.renderDebts(w, r, http.StatusOK, pages.DebtsData{Dialog: "form", Form: f})
	return true
}

// CreateDebt handles the add dialog.
func (h *Handler) CreateDebt(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if h.calculatePayment(w, r, 0) {
		return
	}
	form, d, ok := parseDebtForm(r)
	if !ok {
		h.renderDebts(w, r, http.StatusBadRequest, pages.DebtsData{Dialog: "form", Form: form})
		return
	}
	created, err := h.debts.Create(r.Context(), d)
	if err != nil {
		form.Error = h.userMessage(err)
		h.renderDebts(w, r, statusFor(err), pages.DebtsData{Dialog: "form", Form: form})
		return
	}
	redirectWithNotice(w, r, "added", created.Name)
}

// UpdateDebt handles the edit dialog.
func (h *Handler) UpdateDebt(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()
	if h.calculatePayment(w, r, id) {
		return
	}
	form, d, ok := parseDebtForm(r)
	form.ID, d.ID = id, id
	if !ok {
		h.renderDebts(w, r, http.StatusBadRequest, pages.DebtsData{Dialog: "form", Form: form})
		return
	}
	if err := h.debts.Update(r.Context(), d); err != nil {
		form.Error = h.userMessage(err)
		h.renderDebts(w, r, statusFor(err), pages.DebtsData{Dialog: "form", Form: form})
		return
	}
	redirectWithNotice(w, r, "updated", d.Name)
}

// DeleteDebt removes a debt after confirmation.
func (h *Handler) DeleteDebt(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d, err := h.debts.Get(r.Context(), id)
	if errors.Is(err, service.ErrNotFound) {
		http.Redirect(w, r, "/debts", http.StatusSeeOther) // already gone
		return
	}
	if err == nil {
		err = h.debts.Delete(r.Context(), id)
	}
	if err != nil && !errors.Is(err, service.ErrNotFound) {
		h.errorPage(w, r, "debts", err)
		return
	}
	redirectWithNotice(w, r, "deleted", d.Name)
}

func (h *Handler) loadDebt(w http.ResponseWriter, r *http.Request) (debt.Debt, bool) {
	id, err := idParam(r)
	if err != nil {
		err = service.ErrNotFound
	}
	var d debt.Debt
	if err == nil {
		d, err = h.debts.Get(r.Context(), id)
	}
	if err != nil {
		h.errorPage(w, r, "debts", err)
		return d, false
	}
	return d, true
}

// renderDebts fills in the list and summary, then renders the page with
// whatever dialog data already holds.
func (h *Handler) renderDebts(w http.ResponseWriter, r *http.Request, status int, data pages.DebtsData) {
	stored, err := h.debts.List(r.Context())
	if err != nil {
		h.errorPage(w, r, "debts", err)
		return
	}
	left, err := h.plans.Remaining(r.Context())
	if err != nil {
		h.errorPage(w, r, "debts", err)
		return
	}
	// Cards show what's left (from the plan), with the starting amount below.
	debts := make([]debt.Debt, 0, len(stored))
	var active []debt.Debt // only these count toward the totals
	data.Started = map[int64]debt.Debt{}
	for _, d := range stored {
		data.Started[d.ID] = d
		if l, ok := left[d.ID]; ok {
			d = l
		}
		debts = append(debts, d)
		if d.Inactive {
			data.Inactive++
			continue
		}
		data.TotalBalance += d.Balance
		active = append(active, d)
	}
	data.Shell = h.shell(r, "debts")
	data.Query = strings.TrimSpace(r.URL.Query().Get("q"))
	data.Debts = filterDebts(debts, data.Query)
	data.TotalCount = len(debts)
	data.MinimumTotal = debt.MinimumTotal(active)
	data.Notice = notice(r)
	h.render(w, r, status, pages.DebtsPage(data))
}

func redirectWithNotice(w http.ResponseWriter, r *http.Request, msg, name string) {
	q := url.Values{"msg": {msg}, "name": {name}}
	http.Redirect(w, r, "/debts?"+q.Encode(), http.StatusSeeOther)
}

// filterDebts keeps debts whose name or type contains q, ignoring case.
func filterDebts(debts []debt.Debt, q string) []debt.Debt {
	if q == "" {
		return debts
	}
	q = strings.ToLower(q)
	out := make([]debt.Debt, 0, len(debts))
	for _, d := range debts {
		if strings.Contains(strings.ToLower(d.Name), q) || strings.Contains(strings.ToLower(d.Type), q) {
			out = append(out, d)
		}
	}
	return out
}

// APIMinimumPayment previews the calculated minimum for the add/edit form.
//
//	GET /api/minimum-payment?type=&balance=&apr=&promo=&promo_ends=&term_months=
func (h *Handler) APIMinimumPayment(w http.ResponseWriter, r *http.Request) {
	f := formFromValues(r.URL.Query())
	t, ok := termsFrom(&f)
	amount, rule := minimumText(t, ok)
	value := "" // for filling the loan payment box
	if amount != "—" {
		value = dollarsText(t.Minimum())
	}
	writeJSON(w, http.StatusOK, map[string]string{"amount": amount, "rule": rule, "value": value})
}

func today() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
}

// formFromValues copies the submitted fields so they can be re-shown.
func formFromValues(v url.Values) pages.DebtForm {
	f := pages.DebtForm{
		Name:           strings.TrimSpace(v.Get("name")),
		Type:           v.Get("type"),
		Balance:        strings.TrimSpace(v.Get("balance")),
		APR:            strings.TrimSpace(v.Get("apr")),
		TermMonths:     strings.TrimSpace(v.Get("term_months")),
		MinimumPayment: strings.TrimSpace(v.Get("minimum_payment")),
		DueDay:         strings.TrimSpace(v.Get("due_day")),
		StartDate:      strings.TrimSpace(v.Get("start_date")),
		Active:         v.Get("active") == "1",
		Errors:         map[string]string{},
	}
	remaining, ends := v["promo_remaining"], v["promo_ends"]
	for i := 0; i < max(len(remaining), len(ends)); i++ {
		var row pages.PurchaseRow
		if i < len(remaining) {
			row.Remaining = strings.TrimSpace(remaining[i])
		}
		if i < len(ends) {
			row.Ends = strings.TrimSpace(ends[i])
		}
		f.Purchases = append(f.Purchases, row)
	}
	if len(f.Purchases) == 0 {
		f.Purchases = []pages.PurchaseRow{{}}
	}
	return f
}

// dueDayOf reads the due day field; 0 when not set or invalid.
func dueDayOf(raw string) int {
	day, err := strconv.Atoi(raw)
	if err != nil || day < 1 || day > 31 {
		return 0
	}
	return day
}

// purchasesFrom checks a promotional account's purchase rows. It returns
// the purchases still owing (with how many payments each has before it
// expires) and their total. Blank rows are ignored.
func purchasesFrom(f *pages.DebtForm) (items []debt.PromoItem, parts []debt.PromoPart, total debt.Money, ok bool) {
	ok = true
	due := dueDayOf(f.DueDay)
	for i := range f.Purchases {
		row := &f.Purchases[i]
		row.Error = ""
		if row.Remaining == "" && row.Ends == "" {
			continue
		}
		left, err := debt.ParseDollars(row.Remaining)
		if err != nil || left < 0 || row.Remaining == "" {
			row.Error = "Enter what's left on this purchase."
			ok = false
			continue
		}
		end, err := time.ParseInLocation(time.DateOnly, row.Ends, time.Local)
		if err != nil {
			row.Error = "Pick the date this promotion expires."
			ok = false
			continue
		}
		if left == 0 {
			continue // paid off
		}
		if !end.After(today()) {
			row.Error = "This promotion has expired. Pick a date after today."
			ok = false
			continue
		}
		items = append(items, debt.PromoItem{Remaining: left, Ends: end})
		parts = append(parts, debt.PromoPart{Remaining: left, Payments: max(debt.PaymentsBefore(end, today(), due), 1)})
		total += left
	}
	f.PurchaseTotal = total.String()
	switch {
	case !ok:
		f.Errors["promo"] = "Check the purchases marked below."
	case len(items) == 0:
		f.Errors["promo"] = "Add at least one purchase with an amount left to pay."
		ok = false
	}
	return items, parts, total, ok
}

// termsFrom checks the fields the minimum payment depends on for the chosen
// type, adding messages to f.Errors. ok is true when they are all valid.
func termsFrom(f *pages.DebtForm) (t debt.Terms, ok bool) {
	t.Type = f.Type
	if !debt.ValidType(t.Type) {
		t.Type = debt.DefaultType
	}
	ok = true

	if t.Type == debt.TypePromo {
		// The balance and payment come from the purchases; no interest while they run.
		_, parts, total, good := purchasesFrom(f)
		t.Balance, t.Parts, t.InterestFree = total, parts, true
		return t, good
	}
	if m, err := debt.ParseDollars(f.Balance); err != nil || m <= 0 {
		f.Errors["balance"] = "Enter the balance as a number above 0."
		if t.Type == debt.TypeLoan {
			f.Errors["balance"] = "Enter the remaining principal as a number above 0."
		}
		ok = false
	} else {
		t.Balance = m
	}
	apr, err := strconv.ParseFloat(strings.TrimSuffix(f.APR, "%"), 64)
	if err != nil || apr < 0 || math.IsNaN(apr) || math.IsInf(apr, 0) {
		f.Errors["apr"] = "Enter a number, 0 or higher."
		ok = false
	} else {
		t.APR = apr
	}
	months := func(field, raw string) {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 600 {
			f.Errors[field] = "Enter the months left, 1 or more."
			ok = false
			return
		}
		t.Months = n
	}
	switch {
	case t.Type == debt.TypeLoan:
		if f.TermMonths != "" { // optional: a loan is paid until it's paid off; months only calculate the payment
			months("term_months", f.TermMonths)
		}
	}
	return t, ok
}

func minimumText(t debt.Terms, ok bool) (amount, rule string) {
	isLoan := t.Type == debt.TypeLoan
	if isLoan && ok && t.Months == 0 {
		return "—", "Enter months remaining to calculate the payment."
	}
	if !ok {
		switch {
		case isLoan:
			return "—", "Enter balance, APR and months remaining to calculate."
		case t.InterestFree:
			return "—", "Add each purchase with what's left and when it expires to calculate."
		default:
			return "—", "Enter balance and APR to calculate."
		}
	}
	amount = t.Minimum().String() + "/mo"
	switch {
	case isLoan:
		return amount, "Calculated: pays the loan off over the months remaining."
	case t.InterestFree:
		return amount, "Calculated: clears each purchase before its promotion expires, at least $25."
	default:
		return amount, "Calculated: 1% of balance plus this month's interest, at least $25."
	}
}

// parseDebtForm returns the raw form (for re-rendering), the debt to save,
// and whether every field was valid. The minimum payment is calculated.
func parseDebtForm(r *http.Request) (pages.DebtForm, debt.Debt, bool) {
	_ = r.ParseForm()
	f := formFromValues(r.PostForm)
	t, termsOK := termsFrom(&f)

	if f.Name == "" {
		f.Errors["name"] = "Give this debt a name."
	}
	if !debt.ValidType(f.Type) {
		f.Errors["type"] = "Pick a type from the list."
	}
	due := 0 // not set
	if f.DueDay != "" {
		day, err := strconv.Atoi(f.DueDay)
		if err != nil || day < 1 || day > 31 {
			f.Errors["due_day"] = "Pick a day from 1 to 31, or Not set."
		} else {
			due = day
		}
	}
	// The monthly payment is entered, or calculated when left blank.
	var payment debt.Money
	switch {
	case f.MinimumPayment != "":
		m, err := debt.ParseDollars(f.MinimumPayment)
		if err != nil || m <= 0 {
			f.Errors["minimum_payment"] = "Enter the payment as a number above 0."
		}
		payment = m
	case termsOK && (t.Type != debt.TypeLoan || t.Months > 0):
		payment = t.Minimum()
		f.MinimumPayment = dollarsText(payment)
	case t.Type == debt.TypeLoan && termsOK:
		f.Errors["minimum_payment"] = "Enter the monthly payment, or the months remaining and tap calculate."
	default:
		f.Errors["minimum_payment"] = "Enter the monthly payment, or fill in the fields above and tap calculate."
	}
	start := today()
	if f.StartDate != "" {
		t, err := time.ParseInLocation(time.DateOnly, f.StartDate, time.Local)
		switch {
		case err != nil:
			f.Errors["start_date"] = "Pick the date this balance is from."
		case t.After(today()):
			f.Errors["start_date"] = "Pick today or an earlier date."
		default:
			start = t
		}
	}
	if len(f.Errors) > 0 {
		return f, debt.Debt{}, false
	}

	d := debt.Debt{
		StartDate:      start,
		Name:           f.Name,
		Type:           t.Type,
		Balance:        t.Balance,
		APR:            t.APR,
		MinimumPayment: payment,
		DueDay:         due,
		Inactive:       !f.Active,
	}
	switch {
	case t.Type == debt.TypePromo:
		d.Promos, _, _, _ = purchasesFrom(&f) // already validated; the service sets the balance and end date
	case t.Months > 0:
		end := today().AddDate(0, t.Months, 0)
		d.TermEnds = &end
	}
	return f, d, true
}

// SetDebtActive puts a debt in the plan and Simulate, or leaves it out.
func (h *Handler) SetDebtActive(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()
	active := r.PostFormValue("active") == "1"
	d, err := h.debts.Get(r.Context(), id)
	if err == nil {
		err = h.debts.SetActive(r.Context(), id, active)
	}
	if err != nil {
		h.errorPage(w, r, "debts", err)
		return
	}
	msg := "deactivated"
	if active {
		msg = "activated"
	}
	redirectWithNotice(w, r, msg, d.Name)
}
