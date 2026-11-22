// Command debt-reducer serves the debt payoff planner web app.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lee5i3/debt-reducer/internal/config"
	"github.com/lee5i3/debt-reducer/internal/container"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		return err
	}
	c, err := container.New(cfg)
	if err != nil {
		return err
	}
	return c.Invoke(func(srv *http.Server, db *sql.DB, log *slog.Logger) error {
		defer db.Close()

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if cfg.ExitWithStdin {
			go func() {
				_, _ = io.Copy(io.Discard, os.Stdin)
				stop()
			}()
		}

		// Listen first so port 0 picks a free port; the desktop app reads
		// the real address from the "listening" line.
		ln, err := net.Listen("tcp", srv.Addr)
		if err != nil {
			return err
		}
		errCh := make(chan error, 1)
		go func() {
			log.Info("listening", "addr", ln.Addr().String(), "db", cfg.DBPath)
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- err
			}
			close(errCh)
		}()

		select {
		case err := <-errCh:
			return err
		case <-ctx.Done():
		}

		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	})
}
