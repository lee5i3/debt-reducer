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
}

// Load parses flags, falling back to environment variables and defaults.
//
//	-addr  / DEBT_REDUCER_ADDR  (default ":8080")
//	-db    / DEBT_REDUCER_DB    (default "data/debt-reducer.db")
func Load(args []string) (*Config, error) {
	cfg := &Config{}
	fs := flag.NewFlagSet("debt-reducer", flag.ContinueOnError)
	fs.StringVar(&cfg.Addr, "addr", envOr("DEBT_REDUCER_ADDR", ":8080"), "HTTP listen address")
	fs.StringVar(&cfg.DBPath, "db", envOr("DEBT_REDUCER_DB", "data/debt-reducer.db"), "path to the SQLite database file")
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
