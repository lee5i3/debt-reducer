// Package server builds the chi router and HTTP server.
package server

import (
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/lee5i3/debt-reducer/internal/config"
	"github.com/lee5i3/debt-reducer/internal/handler"
	"github.com/lee5i3/debt-reducer/web"
)

// NewRouter is a dig constructor for the application's routes.
func NewRouter(h *handler.Handler) (http.Handler, error) {
	static, err := fs.Sub(web.Static, "static")
	if err != nil {
		return nil, err
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/healthz", h.Health)
	r.Get("/favicon.ico", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	r.Handle("/static/*", noCache(http.StripPrefix("/static/", http.FileServerFS(static))))

	// Web UI
	// Plan and Simulate need at least one active debt; otherwise they
	// redirect to the debt list.
	r.Group(func(r chi.Router) {
		r.Use(h.RequirePlan)
		r.Get("/", h.PlanPage)
		r.Get("/plan/setup", h.PlanSetupPage)
		r.Post("/plan/setup", h.SavePlanSetup)
		r.Route("/plan/months/{month}", func(r chi.Router) {
			r.Get("/edit", h.EditMonthPage)
			r.Post("/edit", h.SaveMonthEdit)
			r.Post("/{action}", h.MarkMonth)
		})
		r.Get("/simulate", h.SimulatePage)
		r.Post("/simulate/apply", h.ApplySimulation)
		r.Get("/lump-sum", h.LumpSumPage)
	})
	r.Get("/settings", h.SettingsPage)
	r.Post("/settings", h.SaveSettings)
	r.Route("/debts", func(r chi.Router) {
		r.Get("/", h.DebtsPage)
		r.Post("/", h.CreateDebt)
		r.Get("/new", h.NewDebtPage)
		r.Get("/{id}", h.DebtDetailPage)
		r.Post("/{id}", h.UpdateDebt)
		r.Get("/{id}/edit", h.EditDebtPage)
		r.Get("/{id}/delete", h.DeleteDebtPage)
		r.Post("/{id}/delete", h.DeleteDebt)
		r.Post("/{id}/active", h.SetDebtActive)
	})

	// JSON API
	r.Route("/api", func(r chi.Router) {
		r.Route("/debts", func(r chi.Router) {
			r.Get("/", h.APIListDebts)
			r.Post("/", h.APICreateDebt)
			r.Get("/{id}", h.APIGetDebt)
			r.Put("/{id}", h.APIUpdateDebt)
			r.Delete("/{id}", h.APIDeleteDebt)
		})
		r.Get("/settings", h.APIGetSettings)
		r.Put("/settings", h.APISaveSettings)
		r.Get("/preferences", h.APIGetPreferences)
		r.Put("/preferences", h.APISavePreferences)
		r.Get("/plan", h.APIPlan)
		r.Get("/compare", h.APICompare)
		r.Get("/lump-sum", h.APILumpSum)
		r.Get("/minimum-payment", h.APIMinimumPayment)
	})
	return r, nil
}

// NewHTTPServer is a dig constructor for *http.Server.
func NewHTTPServer(cfg *config.Config, router http.Handler, log *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              cfg.Addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelError),
	}
}

// noCache makes browsers fetch static files fresh. They're embedded in the
// binary (no modification time to revalidate against), so without this a
// rebuilt app could keep showing old CSS.
func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}
