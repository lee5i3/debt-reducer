package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/lee5i3/debt-reducer/internal/debt"
	"github.com/lee5i3/debt-reducer/internal/service"
)

const maxBody = 1 << 20

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%w: %w", service.ErrInvalid, err)
	}
	return nil
}

// APIListDebts handles GET /api/debts.
func (h *Handler) APIListDebts(w http.ResponseWriter, r *http.Request) {
	debts, err := h.debts.List(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, debts)
}

// APIGetDebt handles GET /api/debts/{id}.
func (h *Handler) APIGetDebt(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		h.writeError(w, service.ErrNotFound)
		return
	}
	d, err := h.debts.Get(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// APICreateDebt handles POST /api/debts.
func (h *Handler) APICreateDebt(w http.ResponseWriter, r *http.Request) {
	var d debt.Debt
	if err := decode(r, &d); err != nil {
		h.writeError(w, err)
		return
	}
	d.ID = 0
	created, err := h.debts.Create(r.Context(), d)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// APIUpdateDebt handles PUT /api/debts/{id}.
func (h *Handler) APIUpdateDebt(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		h.writeError(w, service.ErrNotFound)
		return
	}
	var d debt.Debt
	if err := decode(r, &d); err != nil {
		h.writeError(w, err)
		return
	}
	d.ID = id
	if err := h.debts.Update(r.Context(), d); err != nil {
		h.writeError(w, err)
		return
	}
	saved, err := h.debts.Get(r.Context(), id) // what was stored, after clean-up
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

// APIDeleteDebt handles DELETE /api/debts/{id}.
func (h *Handler) APIDeleteDebt(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		h.writeError(w, service.ErrNotFound)
		return
	}
	if err := h.debts.Delete(r.Context(), id); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// APIGetSettings handles GET /api/settings.
func (h *Handler) APIGetSettings(w http.ResponseWriter, r *http.Request) {
	s, err := h.debts.Settings(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// APISaveSettings handles PUT /api/settings.
func (h *Handler) APISaveSettings(w http.ResponseWriter, r *http.Request) {
	var s debt.Settings
	if err := decode(r, &s); err != nil {
		h.writeError(w, err)
		return
	}
	if err := h.debts.SaveSettings(r.Context(), s); err != nil {
		h.writeError(w, err)
		return
	}
	saved, err := h.debts.Settings(r.Context()) // e.g. planStart isn't set here
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

// APIPlan handles GET /api/plan?strategy=&budget=&start=.
func (h *Handler) APIPlan(w http.ResponseWriter, r *http.Request) {
	res, ok := h.buildPlan(w, r)
	if ok {
		writeJSON(w, http.StatusOK, res.Plan)
	}
}

// APICompare handles GET /api/compare?budget=&start= (schedules omitted).
func (h *Handler) APICompare(w http.ResponseWriter, r *http.Request) {
	res, ok := h.buildPlan(w, r)
	if !ok {
		return
	}
	for _, p := range res.Comparison {
		p.Schedule = nil
	}
	writeJSON(w, http.StatusOK, res.Comparison)
}

func (h *Handler) buildPlan(w http.ResponseWriter, r *http.Request) (*service.Result, bool) {
	opts, err := planOptions(r)
	if err != nil {
		h.writeError(w, fmt.Errorf("%w: %w", service.ErrInvalid, err))
		return nil, false
	}
	res, err := h.planner.Build(r.Context(), opts)
	if err == nil && len(res.Debts) == 0 {
		err = debt.ErrNoDebts
	}
	if err != nil {
		h.writeError(w, err)
		return nil, false
	}
	return res, true
}
