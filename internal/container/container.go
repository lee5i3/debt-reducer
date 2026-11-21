// Package container builds the dig dependency graph for the application.
package container

import (
	"log/slog"
	"os"

	"go.uber.org/dig"

	"github.com/lee5i3/debt-reducer/internal/config"
	"github.com/lee5i3/debt-reducer/internal/debt"
	"github.com/lee5i3/debt-reducer/internal/handler"
	"github.com/lee5i3/debt-reducer/internal/repository"
	"github.com/lee5i3/debt-reducer/internal/server"
	"github.com/lee5i3/debt-reducer/internal/service"
	"github.com/lee5i3/debt-reducer/internal/storage"
)

// New registers every constructor with a new dig container.
func New(cfg *config.Config) (*dig.Container, error) {
	c := dig.New()

	providers := []any{
		func() *config.Config { return cfg },
		newLogger,
		storage.Open,
		repository.NewDebtRepository,
		repository.NewSettingsRepository,
		repository.NewPlanRepository,
		debt.NewCalculator,
		service.NewDebtService,
		service.NewPlanner,
		service.NewPlanService,
		handler.New,
		server.NewRouter,
		server.NewHTTPServer,
	}
	for _, p := range providers {
		if err := c.Provide(p); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func newLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, nil))
}
