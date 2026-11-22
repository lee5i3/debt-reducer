// Package config holds process-level settings (listen address, database
// path). User data such as debts lives in the database, not here.
package config

import (
	"flag"
	"os"
)

// Config is the application configuration.
type Config struct {
	Addr   string
	DBPath string
	// ExitWithStdin stops the server when stdin closes, so it doesn't
	// outlive the desktop app that started it.
	ExitWithStdin bool
}

// Load parses flags, falling back to environment variables and defaults.
//
//	-addr  / DEBT_REDUCER_ADDR  (default ":8080")
//	-db    / DEBT_REDUCER_DB    (default "data/debt-reducer.db")
//	-exit-with-stdin            (stop when stdin closes; used by the desktop app)
func Load(args []string) (*Config, error) {
	cfg := &Config{}
	fs := flag.NewFlagSet("debt-reducer", flag.ContinueOnError)
	fs.StringVar(&cfg.Addr, "addr", envOr("DEBT_REDUCER_ADDR", ":8080"), "HTTP listen address")
	fs.StringVar(&cfg.DBPath, "db", envOr("DEBT_REDUCER_DB", "data/debt-reducer.db"), "path to the SQLite database file")
	fs.BoolVar(&cfg.ExitWithStdin, "exit-with-stdin", false, "stop when stdin closes (used by the desktop app)")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
