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
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServerFS(static)))

	// Web UI
	r.Get("/", h.Dashboard)
	r.Post("/settings", h.SaveSettings)
	r.Get("/lump-sum", h.LumpSumPage)
	r.Route("/debts", func(r chi.Router) {
		r.Get("/", h.DebtsPage)
		r.Post("/", h.CreateDebt)
		r.Get("/{id}/edit", h.EditDebtPage)
		r.Post("/{id}", h.UpdateDebt)
		r.Post("/{id}/delete", h.DeleteDebt)
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
		r.Get("/plan", h.APIPlan)
		r.Get("/compare", h.APICompare)
		r.Get("/lump-sum", h.APILumpSum)
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
