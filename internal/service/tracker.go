package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/lee5i3/debt-reducer/internal/debt"
	"github.com/lee5i3/debt-reducer/internal/repository"
)

// PlanMonth is one month of the tracked plan: either history (marked paid or
// skipped) or projected from today's balances.
type PlanMonth struct {
	Month    time.Time
	Status   string     // "", debt.MonthPaid or debt.MonthSkipped
	Next     bool       // the next month to mark
	Edited   bool       // payment was changed for this month
	Extra    debt.Money // one-time payment added to this month (removable)
	CanUndo  bool       // the most recently marked month
	Paid     debt.Money
	Interest debt.Money
	Left     debt.Money // total still owed after the month
	Debts    []debt.MonthDebt
}

// PaysOff lists the debts paid off in this month.
func (m PlanMonth) PaysOff() []string {
	var names []string
	for _, d := range m.Debts {
		if d.Role == debt.RolePaidOff {
			names = append(names, d.Name)
		}
	}
	return names
}

// Upcoming reports whether the month hasn't been marked yet.
func (m PlanMonth) Upcoming() bool { return m.Status == "" }

// TrackedPlan is the saved plan with its history and projection.
type TrackedPlan struct {
	Set           bool // false until the user sets up a plan
	Strategy      debt.Strategy
	Order         debt.Order // for a custom plan
	MonthlyBudget debt.Money
	MinimumTotal  debt.Money
	Extra         debt.Money // on top of the minimums (0 with a fixed total)
	FixedTotal    bool       // saved as a fixed monthly total (older plans, API)
	Start         time.Time
	StartTotal    debt.Money
	Owed          debt.Money // current total of all balances
	Months        []PlanMonth
	PaidSoFar     debt.Money
	PaidCount     int
	MarkedCount   int
	Changes       []debt.BudgetChange // scheduled monthly amounts after the next month
	Error         string              // why the projection couldn't be calculated
}

// Next returns the next month to mark, or nil when everything is paid.
func (p *TrackedPlan) Next() *PlanMonth {
	for i := range p.Months {
		if p.Months[i].Next {
			return &p.Months[i]
		}
	}
	return nil
}

// DebtFree is the last month of the plan (zero if there is none).
func (p *TrackedPlan) DebtFree() time.Time {
	if len(p.Months) == 0 {
		return time.Time{}
	}
	return p.Months[len(p.Months)-1].Month
}

// MonthsLeft counts the months not yet marked.
func (p *TrackedPlan) MonthsLeft() int { return len(p.Months) - p.MarkedCount }

// InterestPaid is the interest charged in the months already marked
// (paid or skipped).
func (p *TrackedPlan) InterestPaid() debt.Money {
	var total debt.Money
	for _, m := range p.Months {
		if !m.Upcoming() {
			total += m.Interest
		}
	}
	return total
}

// InterestAhead is the interest the rest of the plan is projected to cost.
func (p *TrackedPlan) InterestAhead() debt.Money {
	var total debt.Money
	for _, m := range p.Months {
		if m.Upcoming() {
			total += m.Interest
		}
	}
	return total
}

// Projection compares the plan with and without a simulated change.
type Projection struct {
	Next    time.Time // first month of both schedules
	Base    *debt.Plan
	Changed *debt.Plan
}

// PlanService manages the saved, tracked plan.
type PlanService interface {
	Tracked(ctx context.Context) (*TrackedPlan, error)
	// Setup saves the plan. A budget above 0 is a fixed monthly total;
	// otherwise the plan pays the minimums plus extra.
	Setup(ctx context.Context, s debt.Strategy, o debt.Order, budget, extra debt.Money, start time.Time) error
	Mark(ctx context.Context, month time.Time, status string) error
	Undo(ctx context.Context, month time.Time) error
	// SetOverride changes (or with nil resets) an upcoming month's payment.
	SetOverride(ctx context.Context, month time.Time, amount *debt.Money) error
	// BudgetFor is the planned payment for an upcoming month, not counting
	// any one-time payment added to it.
	BudgetFor(ctx context.Context, month time.Time) (debt.Money, error)
	// PlanFor is a month's amount from the plan setup (and any scheduled
	// change), before edits and one-time payments.
	PlanFor(ctx context.Context, month time.Time) (debt.Money, error)
	// SetAllocations sets (or with nil clears) what each account gets in an
	// upcoming month; it replaces any edited total for that month.
	SetAllocations(ctx context.Context, month time.Time, amounts map[int64]debt.Money) error
	// AddExtra adds a one-time payment to an upcoming month.
	AddExtra(ctx context.Context, month time.Time, amount debt.Money) error
	// RemoveExtra removes an upcoming month's one-time payment.
	RemoveExtra(ctx context.Context, month time.Time) error
	// RaiseMonthly adds amount to the monthly payment from month on. From
	// the next unpaid month it updates the plan setup; later, it is
	// scheduled so earlier months keep their amount. Marked months never change.
	RaiseMonthly(ctx context.Context, from time.Time, amount debt.Money) error
	// Remaining returns every debt with what's left on it, worked out from
	// its starting amount and the marked months.
	Remaining(ctx context.Context) (map[int64]debt.Debt, error)
	// Simulate projects the plan with extra money in some months.
	Simulate(ctx context.Context, extra func(i int) debt.Money) (*Projection, error)
}

type planService struct {
	debts    repository.DebtRepository
	settings repository.SettingsRepository
	months   repository.PlanRepository
	calc     *debt.Calculator
	now      func() time.Time
}

// NewPlanService is a dig constructor for PlanService.
func NewPlanService(debts repository.DebtRepository, settings repository.SettingsRepository, months repository.PlanRepository, calc *debt.Calculator) PlanService {
	return &planService{debts: debts, settings: settings, months: months, calc: calc, now: time.Now}
}

// FirstOfMonth returns midnight on the first day of t's month.
func FirstOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.Local)
}

// state is everything loaded from storage for one request.
type state struct {
	set       bool
	settings  debt.Settings
	active    []debt.Debt // balance above zero
	start     time.Time
	marked    []debt.MonthRecord
	history   [][]debt.MonthDebt  // per marked month, replayed from starting amounts
	remaining map[int64]debt.Debt // every debt with what's left
	overrides map[time.Time]debt.Money
	extras    map[time.Time]debt.Money
	allocs    map[time.Time]map[int64]debt.Money // edited months: per-account amounts
	changes   []debt.BudgetChange                // scheduled monthly amounts, earliest first
	next      time.Time                          // next month to mark
}

func (s *state) budget() debt.Money {
	if s.settings.MonthlyBudget > 0 {
		return s.settings.MonthlyBudget // a fixed total
	}
	return debt.MinimumTotal(s.active) + s.settings.Extra
}

// startTotal is what the active debts owed when the plan started: each
// debt's balance going into its first applied plan month, or what's left
// now if no month has touched it yet. Inactive debts don't count, so the
// progress bar only measures debts in the plan.
func (s *state) startTotal() debt.Money {
	first := map[int64]debt.Money{}
	for i, rows := range s.history {
		for _, r := range rows {
			d, ok := s.remaining[r.DebtID]
			if _, done := first[r.DebtID]; done || !ok || s.marked[i].Month.Before(FirstOfMonth(d.StartDate)) {
				continue
			}
			first[r.DebtID] = r.Before
		}
	}
	var total debt.Money
	for id, d := range s.remaining {
		if d.Inactive {
			continue
		}
		if b, ok := first[id]; ok {
			total += b
		} else {
			total += d.Balance
		}
	}
	return total
}

func (s *state) strategy() debt.Strategy {
	if s.settings.Strategy.Valid() {
		return s.settings.Strategy
	}
	return debt.Avalanche
}

// baseFor is the monthly amount for a month from the setup and any
// scheduled change, before edits and one-time payments.
func (s *state) baseFor(month time.Time) debt.Money {
	b := s.budget()
	for _, c := range s.changes {
		if !c.Month.After(month) {
			b = c.Amount
		}
	}
	return b
}

// budgetFor is the planned payment for a month, not counting a one-time payment.
func (s *state) budgetFor(month time.Time) debt.Money {
	if a, ok := s.allocs[month]; ok {
		var total debt.Money
		for _, v := range a {
			total += v
		}
		return total
	}
	if o, ok := s.overrides[month]; ok {
		return o
	}
	return s.baseFor(month)
}

// edited reports whether a month's payment was changed (total or per account).
func (s *state) edited(month time.Time) bool {
	_, o := s.overrides[month]
	_, a := s.allocs[month]
	return o || a
}

func (p *planService) load(ctx context.Context) (*state, error) {
	set, err := p.settings.Get(ctx)
	if err != nil {
		return nil, err
	}
	all, err := p.debts.List(ctx)
	if err != nil {
		return nil, err
	}
	months, err := p.months.Months(ctx)
	if err != nil {
		return nil, err
	}
	changes, err := p.months.BudgetChanges(ctx)
	if err != nil {
		return nil, err
	}
	allocs, err := p.months.Allocations(ctx)
	if err != nil {
		return nil, err
	}
	st := &state{settings: set, changes: changes, allocs: allocs, overrides: map[time.Time]debt.Money{}, extras: map[time.Time]debt.Money{}}
	st.set = set.PlanStart != nil
	st.start = FirstOfMonth(p.now())
	if st.set {
		st.start = FirstOfMonth(*set.PlanStart)
	}
	for _, m := range months {
		if m.Marked() {
			st.marked = append(st.marked, m)
		}
		if m.Override != nil {
			st.overrides[m.Month] = *m.Override
		}
		if m.Extra != nil {
			st.extras[m.Month] = *m.Extra
		}
	}
	st.next = st.start.AddDate(0, len(st.marked), 0)

	// What's left on each debt comes from its starting amount and the
	// marked months; only active debts with something left are in the plan.
	st.remaining, st.history = replay(all, st.marked)
	for _, d := range all {
		if r := st.remaining[d.ID]; r.Balance > 0 && !d.Inactive {
			st.active = append(st.active, r)
		}
	}
	return st, nil
}

// project runs the calculator from today's balances, starting at the next
// month to mark, with edited months and any extra money applied.
func (p *planService) project(st *state, extra func(i int) debt.Money) (*debt.Plan, error) {
	if len(st.active) == 0 {
		return nil, nil
	}
	req := debt.PlanRequest{
		Debts:         st.active,
		MonthlyBudget: st.budget(),
		Strategy:      st.strategy(),
		Order:         st.settings.Order.Normalize(),
		StartDate:     st.next,
		Adjust: func(period int, _ debt.Money) debt.Money {
			month := st.next.AddDate(0, period, 0)
			b := st.budgetFor(month) + st.extras[month]
			if extra != nil {
				b += extra(period)
			}
			return b
		},
		Allocate: func(period int) map[int64]debt.Money {
			return st.allocs[st.next.AddDate(0, period, 0)]
		},
	}
	return p.calc.Plan(req)
}

// monthDebts turns one projected period into per-debt rows.
func monthDebts(m debt.Month, mins map[int64]debt.Money) []debt.MonthDebt {
	out := make([]debt.MonthDebt, 0, len(m.Payments))
	for _, pay := range m.Payments {
		role := debt.RoleMinimum
		switch {
		case pay.PaidOff:
			role = debt.RolePaidOff
		case pay.Amount > mins[pay.DebtID]:
			role = debt.RoleExtra
		}
		out = append(out, debt.MonthDebt{
			DebtID:   pay.DebtID,
			Name:     pay.Debt,
			Before:   pay.Remaining + pay.Amount - pay.Interest,
			Interest: pay.Interest,
			Paid:     pay.Amount,
			Minimum:  min(mins[pay.DebtID], pay.Amount),
			Left:     pay.Remaining,
			Role:     role,
		})
	}
	return out
}

func minimums(debts []debt.Debt) map[int64]debt.Money {
	m := make(map[int64]debt.Money, len(debts))
	for _, d := range debts {
		m[d.ID] = d.MinimumPayment
	}
	return m
}

func (p *planService) Tracked(ctx context.Context) (*TrackedPlan, error) {
	st, err := p.load(ctx)
	if err != nil {
		return nil, err
	}
	tp := &TrackedPlan{
		Set:           st.set,
		Strategy:      st.strategy(),
		Order:         st.settings.Order.Normalize(),
		MonthlyBudget: st.budget(),
		MinimumTotal:  debt.MinimumTotal(st.active),
		Extra:         st.settings.Extra,
		FixedTotal:    st.settings.MonthlyBudget > 0,
		Start:         st.start,
		StartTotal:    st.startTotal(),
		MarkedCount:   len(st.marked),
	}
	for _, c := range st.changes {
		if c.Month.After(st.next) {
			tp.Changes = append(tp.Changes, c)
		}
	}
	for _, d := range st.active {
		tp.Owed += d.Balance
	}
	if tp.StartTotal == 0 {
		tp.StartTotal = tp.Owed
	}

	for i, m := range st.marked {
		var left, paid, interest debt.Money
		for _, d := range st.history[i] {
			left += d.Left
			paid += d.Paid
			interest += d.Interest
		}
		tp.Months = append(tp.Months, PlanMonth{
			Month: m.Month, Status: m.Status, Edited: st.edited(m.Month), Extra: st.extras[m.Month], CanUndo: i == len(st.marked)-1,
			Paid: paid, Interest: interest, Left: left, Debts: st.history[i],
		})
		m.Paid = paid
		if m.Status == debt.MonthPaid {
			tp.PaidSoFar += m.Paid
			tp.PaidCount++
		}
	}

	plan, err := p.project(st, nil)
	if err != nil {
		tp.Error = err.Error()
		return tp, nil
	}
	if plan == nil {
		return tp, nil
	}
	mins := minimums(st.active)
	for i, m := range plan.Schedule {
		month := st.next.AddDate(0, i, 0)
		tp.Months = append(tp.Months, PlanMonth{
			Month: month, Next: i == 0, Edited: st.edited(month), Extra: st.extras[month],
			Paid: m.TotalPaid, Interest: m.TotalInterest, Left: m.RemainingBalance,
			Debts: monthDebts(m, mins),
		})
	}
	return tp, nil
}

func (p *planService) Setup(ctx context.Context, s debt.Strategy, o debt.Order, budget, extra debt.Money, start time.Time) error {
	if !s.Valid() {
		return invalid(fmt.Errorf("unknown strategy %q", s))
	}
	st, err := p.load(ctx)
	if err != nil {
		return err
	}
	if min := debt.MinimumTotal(st.active); s != debt.MinimumOnly && budget > 0 && budget < min {
		return invalid(fmt.Errorf("the monthly amount must cover your minimums (%s)", min))
	}
	if budget < 0 || extra < 0 {
		return invalid(errors.New("the monthly amount can't be negative"))
	}
	if budget > 0 {
		extra = 0
	}
	cur := st.settings
	if (budget != cur.MonthlyBudget || extra != cur.Extra) && len(st.changes) > 0 {
		// A new monthly amount in setup replaces any scheduled changes.
		if err := p.months.SetBudgetChanges(ctx, nil); err != nil {
			return err
		}
	}
	cur.Strategy, cur.Order, cur.MonthlyBudget, cur.Extra = s, o.Normalize(), budget, extra
	if err := p.settings.Save(ctx, cur); err != nil {
		return err
	}
	// The first month can only change before any month is marked.
	if len(st.marked) > 0 && st.set {
		return nil
	}
	var owed debt.Money
	for _, d := range st.active {
		owed += d.Balance
	}
	return p.settings.SavePlanStart(ctx, FirstOfMonth(start), owed)
}

func (p *planService) Mark(ctx context.Context, month time.Time, status string) error {
	st, err := p.load(ctx)
	if err != nil {
		return err
	}
	if !st.set {
		return invalid(errors.New("set up your plan first"))
	}
	month = FirstOfMonth(month)
	if !month.Equal(st.next) {
		return invalid(fmt.Errorf("mark %s first", st.next.Format("January 2006")))
	}
	if len(st.active) == 0 {
		return invalid(errors.New("there's nothing left to pay"))
	}
	rec := debt.MonthRecord{Month: month, Status: status}
	switch status {
	case debt.MonthPaid:
		plan, err := p.project(st, nil)
		if err != nil {
			return err
		}
		m := plan.Schedule[0]
		rec.Paid, rec.Interest = m.TotalPaid, m.TotalInterest
		rec.Debts = monthDebts(m, minimums(st.active))
	case debt.MonthSkipped:
		for _, d := range st.active {
			interest := debt.MonthlyInterest(d.Balance, d.APR)
			rec.Interest += interest
			rec.Debts = append(rec.Debts, debt.MonthDebt{
				DebtID: d.ID, Name: d.Name, Before: d.Balance, Interest: interest,
				Left: d.Balance + interest, Role: debt.RoleSkipped,
			})
		}
	default:
		return invalid(fmt.Errorf("unknown status %q", status))
	}
	return p.months.Record(ctx, rec)
}

func (p *planService) Undo(ctx context.Context, month time.Time) error {
	st, err := p.load(ctx)
	if err != nil {
		return err
	}
	if len(st.marked) == 0 || !st.marked[len(st.marked)-1].Month.Equal(FirstOfMonth(month)) {
		return invalid(errors.New("only the most recently marked month can be undone"))
	}
	return p.months.Undo(ctx, FirstOfMonth(month))
}

func (p *planService) SetOverride(ctx context.Context, month time.Time, amount *debt.Money) error {
	st, err := p.load(ctx)
	if err != nil {
		return err
	}
	month = FirstOfMonth(month)
	if month.Before(st.next) {
		return invalid(errors.New("that month is already marked"))
	}
	if amount != nil && *amount < 0 {
		return invalid(errors.New("the payment can't be negative"))
	}
	return p.months.SetOverride(ctx, month, amount)
}

func (p *planService) PlanFor(ctx context.Context, month time.Time) (debt.Money, error) {
	st, err := p.load(ctx)
	if err != nil {
		return 0, err
	}
	return st.baseFor(FirstOfMonth(month)), nil
}

func (p *planService) BudgetFor(ctx context.Context, month time.Time) (debt.Money, error) {
	st, err := p.load(ctx)
	if err != nil {
		return 0, err
	}
	return st.budgetFor(FirstOfMonth(month)), nil
}

func (p *planService) Simulate(ctx context.Context, extra func(i int) debt.Money) (*Projection, error) {
	st, err := p.load(ctx)
	if err != nil {
		return nil, err
	}
	base, err := p.project(st, nil)
	if err != nil {
		return nil, err
	}
	changed, err := p.project(st, extra)
	if err != nil {
		return nil, err
	}
	return &Projection{Next: st.next, Base: base, Changed: changed}, nil
}

func (p *planService) AddExtra(ctx context.Context, month time.Time, amount debt.Money) error {
	st, err := p.load(ctx)
	if err != nil {
		return err
	}
	month = FirstOfMonth(month)
	if month.Before(st.next) {
		return invalid(errors.New("that month is already marked"))
	}
	if amount <= 0 {
		return invalid(errors.New("the one-time payment must be above $0"))
	}
	total := st.extras[month] + amount
	return p.months.SetExtra(ctx, month, &total)
}

func (p *planService) RemoveExtra(ctx context.Context, month time.Time) error {
	st, err := p.load(ctx)
	if err != nil {
		return err
	}
	month = FirstOfMonth(month)
	if month.Before(st.next) {
		return invalid(errors.New("that month is already marked"))
	}
	return p.months.SetExtra(ctx, month, nil)
}

func (p *planService) RaiseMonthly(ctx context.Context, from time.Time, amount debt.Money) error {
	st, err := p.load(ctx)
	if err != nil {
		return err
	}
	if !st.set {
		return invalid(errors.New("set up your plan first"))
	}
	if amount <= 0 {
		return invalid(errors.New("the extra must be above $0"))
	}
	from = FirstOfMonth(from)
	if from.Before(st.next) {
		from = st.next // marked months never change
	}
	// Scheduled changes on or after the start go up too; earlier ones stay.
	var changes []debt.BudgetChange
	for _, c := range st.changes {
		if !c.Month.Before(from) {
			c.Amount += amount
		}
		if c.Month.After(st.next) {
			changes = append(changes, c)
		}
	}
	if from.Equal(st.next) {
		// From the next unpaid month: this is the plan's monthly amount now.
		cur := st.settings
		if cur.MonthlyBudget > 0 {
			cur.MonthlyBudget = st.baseFor(st.next) + amount
		} else {
			cur.Extra += amount
		}
		if err := p.settings.Save(ctx, cur); err != nil {
			return err
		}
	} else if !hasChange(changes, from) {
		changes = append(changes, debt.BudgetChange{Month: from, Amount: st.baseFor(from) + amount})
		sort.Slice(changes, func(i, j int) bool { return changes[i].Month.Before(changes[j].Month) })
	}
	return p.months.SetBudgetChanges(ctx, changes)
}

func hasChange(changes []debt.BudgetChange, month time.Time) bool {
	for _, c := range changes {
		if c.Month.Equal(month) {
			return true
		}
	}
	return false
}

func (p *planService) SetAllocations(ctx context.Context, month time.Time, amounts map[int64]debt.Money) error {
	st, err := p.load(ctx)
	if err != nil {
		return err
	}
	month = FirstOfMonth(month)
	if month.Before(st.next) {
		return invalid(errors.New("that month is already marked"))
	}
	for _, v := range amounts {
		if v < 0 {
			return invalid(errors.New("payments can't be negative"))
		}
	}
	if _, ok := st.overrides[month]; ok {
		if err := p.months.SetOverride(ctx, month, nil); err != nil {
			return err
		}
	}
	if len(amounts) == 0 {
		amounts = nil
	}
	return p.months.SetAllocations(ctx, month, amounts)
}

func (p *planService) Remaining(ctx context.Context) (map[int64]debt.Debt, error) {
	st, err := p.load(ctx)
	if err != nil {
		return nil, err
	}
	return st.remaining, nil
}
