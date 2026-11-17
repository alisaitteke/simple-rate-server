package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alisaitteke/simple-rate-server/internal/model"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for k := range envKeys {
		t.Setenv(k, "")
	}
}

func TestDefaults(t *testing.T) {
	clearEnv(t)
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 8080 {
		t.Fatalf("port = %d", cfg.Port)
	}
	if len(cfg.Sources) != 7 {
		t.Fatalf("sources = %v", cfg.Sources)
	}
	if len(cfg.Crypto) != 12 {
		t.Fatalf("crypto = %v", cfg.Crypto)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("port: 7000\ncrypto: [BTC, ETH, SOL]\nlog_level: debug\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RATE_PORT", "9999")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 9999 {
		t.Fatalf("port = %d, want env override 9999", cfg.Port)
	}
	if len(cfg.Crypto) != 3 {
		t.Fatalf("crypto = %v", cfg.Crypto)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("log level = %s", cfg.LogLevel)
	}
}

func TestUnknownKeyRejected(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("nope: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error for an unknown key")
	}
}

func TestDisableCrypto(t *testing.T) {
	clearEnv(t)
	t.Setenv("RATE_CRYPTO", "none")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Crypto) != 0 {
		t.Fatalf("crypto = %v, want empty", cfg.Crypto)
	}
}

func TestStrategyDefaults(t *testing.T) {
	clearEnv(t)
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Strategy != model.StrategyPriority ||
		cfg.StrategyFiat != model.StrategyPriority ||
		cfg.StrategyCrypto != model.StrategyPriority {
		t.Fatalf("strategies = %q/%q/%q", cfg.Strategy, cfg.StrategyFiat, cfg.StrategyCrypto)
	}
}

func TestStrategyInheritance(t *testing.T) {
	clearEnv(t)
	t.Setenv("RATE_STRATEGY", "max")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Strategy != model.StrategyMax ||
		cfg.StrategyFiat != model.StrategyMax ||
		cfg.StrategyCrypto != model.StrategyMax {
		t.Fatalf("strategies = %q/%q/%q", cfg.Strategy, cfg.StrategyFiat, cfg.StrategyCrypto)
	}
}

func TestStrategyPerKindOverride(t *testing.T) {
	clearEnv(t)
	t.Setenv("RATE_STRATEGY", "max")
	t.Setenv("RATE_STRATEGY_CRYPTO", "min")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.StrategyFiat != model.StrategyMax || cfg.StrategyCrypto != model.StrategyMin {
		t.Fatalf("strategies = %q/%q, want max/min", cfg.StrategyFiat, cfg.StrategyCrypto)
	}
}

func TestInvalidStrategyRejected(t *testing.T) {
	clearEnv(t)
	t.Setenv("RATE_STRATEGY", "best")
	if _, err := Load(""); err == nil {
		t.Fatal("unknown strategy must be rejected")
	}
}

func TestStrategyAliases(t *testing.T) {
	cases := map[string]model.Strategy{
		"highest": model.StrategyMax,
		"lowest":  model.StrategyMin,
		"average": model.StrategyAvg,
		"mean":    model.StrategyAvg,
	}
	for input, want := range cases {
		clearEnv(t)
		t.Setenv("RATE_STRATEGY", input)
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("%s: Load: %v", input, err)
		}
		if cfg.Strategy != want {
			t.Fatalf("%s: strategy = %q, want %q", input, cfg.Strategy, want)
		}
	}
}

func TestInvalidValuesRejected(t *testing.T) {
	clearEnv(t)
	t.Setenv("RATE_PORT", "0")
	if _, err := Load(""); err == nil {
		t.Fatal("port 0 must be rejected")
	}

	clearEnv(t)
	t.Setenv("RATE_SOURCES", "ecb,nope")
	if _, err := Load(""); err == nil {
		t.Fatal("unknown source must be rejected")
	}

	clearEnv(t)
	t.Setenv("RATE_POLL_MARKET", "soon")
	if _, err := Load(""); err == nil {
		t.Fatal("invalid duration must be rejected")
	}
}
