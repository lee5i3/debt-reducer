package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DEBT_REDUCER_ADDR", "")
	t.Setenv("DEBT_REDUCER_DB", "")
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8080" || cfg.DBPath != "data/debt-reducer.db" || cfg.ExitWithStdin {
		t.Fatalf("got %+v", cfg)
	}
}

func TestLoadEnvAndFlags(t *testing.T) {
	t.Setenv("DEBT_REDUCER_ADDR", ":9000")
	t.Setenv("DEBT_REDUCER_DB", "env.db")
	cfg, err := Load([]string{"-addr", "127.0.0.1:0", "-exit-with-stdin"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:0" {
		t.Errorf("Addr = %q, want the flag to win over the environment", cfg.Addr)
	}
	if cfg.DBPath != "env.db" {
		t.Errorf("DBPath = %q, want the environment value", cfg.DBPath)
	}
	if !cfg.ExitWithStdin {
		t.Error("ExitWithStdin = false, want true")
	}
}

func TestLoadRejectsUnknownFlag(t *testing.T) {
	if _, err := Load([]string{"-nope"}); err == nil {
		t.Fatal("want an error for an unknown flag")
	}
}
