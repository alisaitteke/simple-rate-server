// Package config loads server configuration from defaults, an optional YAML
// file and RATE_* environment variables (in that order of precedence).
package config

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/alisaitteke/simple-rate-server/internal/model"
)

// Config is the fully resolved server configuration.
type Config struct {
	Port            int
	Bind            string
	Sources         []string
	Fiat            []string
	Crypto          []string
	PollMarket      time.Duration
	PollTCMB        time.Duration
	PollFiat        time.Duration
	PollCoinGecko   time.Duration
	PollFawaz       time.Duration
	StaleMarket     time.Duration
	StaleOfficial   time.Duration
	StaleAggregator time.Duration
	Strategy        model.Strategy
	StrategyFiat    model.Strategy
	StrategyCrypto  model.Strategy
	StartupTimeout  time.Duration
	LogFormat       string
	LogLevel        string
	CORS            bool
}

var knownSources = map[string]bool{
	"ecb":        true,
	"erapi":      true,
	"tcmb":       true,
	"binance":    true,
	"coinbase":   true,
	"coingecko":  true,
	"fawazahmed": true,
}

// Default returns the zero-configuration defaults.
func Default() Config {
	return Config{
		Port:            8080,
		Bind:            "",
		Sources:         []string{"ecb", "erapi", "tcmb", "binance", "coinbase", "coingecko", "fawazahmed"},
		Fiat:            []string{"*"},
		Crypto:          []string{"BTC", "ETH", "USDT", "USDC", "SOL", "XRP", "ADA", "DOGE", "AVAX", "DOT", "BNB", "LTC"},
		PollMarket:      30 * time.Second,
		PollTCMB:        time.Hour,
		PollFiat:        6 * time.Hour,
		PollCoinGecko:   5 * time.Minute,
		PollFawaz:       12 * time.Hour,
		StaleMarket:     5 * time.Minute,
		StaleOfficial:   96 * time.Hour,
		StaleAggregator: 48 * time.Hour,
		Strategy:        model.StrategyPriority,
		StartupTimeout:  15 * time.Second,
		LogFormat:       "json",
		LogLevel:        "info",
		CORS:            true,
	}
}

// Load builds a configuration starting from defaults.
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		if err := applyFile(&cfg, path); err != nil {
			return cfg, err
		}
	}
	if err := applyEnv(&cfg); err != nil {
		return cfg, err
	}
	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// envKeys maps environment variables to configuration keys.
var envKeys = map[string]string{
	"RATE_PORT":             "port",
	"RATE_BIND":             "bind",
	"RATE_SOURCES":          "sources",
	"RATE_FIAT":             "fiat",
	"RATE_CRYPTO":           "crypto",
	"RATE_POLL_MARKET":      "poll_market",
	"RATE_POLL_TCMB":        "poll_tcmb",
	"RATE_POLL_FIAT":        "poll_fiat",
	"RATE_POLL_COINGECKO":   "poll_coingecko",
	"RATE_POLL_FAWAZ":       "poll_fawaz",
	"RATE_STALE_MARKET":     "stale_market",
	"RATE_STALE_OFFICIAL":   "stale_official",
	"RATE_STALE_AGGREGATOR": "stale_aggregator",
	"RATE_STRATEGY":         "strategy",
	"RATE_STRATEGY_FIAT":    "strategy_fiat",
	"RATE_STRATEGY_CRYPTO":  "strategy_crypto",
	"RATE_STARTUP_TIMEOUT":  "startup_timeout",
	"RATE_LOG_FORMAT":       "log_format",
	"RATE_LOG_LEVEL":        "log_level",
	"RATE_CORS":             "cors",
}

func applyEnv(cfg *Config) error {
	// Sort keys so error messages are deterministic.
	keys := make([]string, 0, len(envKeys))
	for k := range envKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, env := range keys {
		v, ok := os.LookupEnv(env)
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		if err := setValue(cfg, envKeys[env], v); err != nil {
			return fmt.Errorf("%s: %w", env, err)
		}
	}
	return nil
}

func applyFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse config %s: %w", path, err)
	}
	for k, v := range raw {
		key := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(k), "-", "_"))
		if err := setValue(cfg, key, valueString(v)); err != nil {
			return fmt.Errorf("config %s: %w", k, err)
		}
	}
	return nil
}

func valueString(v any) string {
	switch t := v.(type) {
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			parts = append(parts, fmt.Sprint(item))
		}
		return strings.Join(parts, ",")
	default:
		return fmt.Sprint(v)
	}
}

func setValue(cfg *Config, key, value string) error {
	value = strings.TrimSpace(value)
	switch key {
	case "port":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid integer %q", value)
		}
		cfg.Port = n
	case "bind":
		cfg.Bind = value
	case "sources":
		cfg.Sources = splitList(value)
	case "fiat":
		cfg.Fiat = parseList(value)
	case "crypto":
		cfg.Crypto = parseList(value)
	case "poll_market":
		return setDuration(&cfg.PollMarket, value)
	case "poll_tcmb":
		return setDuration(&cfg.PollTCMB, value)
	case "poll_fiat":
		return setDuration(&cfg.PollFiat, value)
	case "poll_coingecko":
		return setDuration(&cfg.PollCoinGecko, value)
	case "poll_fawaz":
		return setDuration(&cfg.PollFawaz, value)
	case "stale_market":
		return setDuration(&cfg.StaleMarket, value)
	case "stale_official":
		return setDuration(&cfg.StaleOfficial, value)
	case "stale_aggregator":
		return setDuration(&cfg.StaleAggregator, value)
	case "strategy":
		cfg.Strategy = model.Strategy(strings.ToLower(value))
	case "strategy_fiat":
		cfg.StrategyFiat = model.Strategy(strings.ToLower(value))
	case "strategy_crypto":
		cfg.StrategyCrypto = model.Strategy(strings.ToLower(value))
	case "startup_timeout":
		return setDuration(&cfg.StartupTimeout, value)
	case "log_format":
		cfg.LogFormat = strings.ToLower(value)
	case "log_level":
		cfg.LogLevel = strings.ToLower(value)
	case "cors":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid boolean %q", value)
		}
		cfg.CORS = b
	default:
		return fmt.Errorf("unknown key")
	}
	return nil
}

func setDuration(dst *time.Duration, value string) error {
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return fmt.Errorf("invalid duration %q", value)
	}
	*dst = d
	return nil
}

func splitList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseList accepts a comma list or an explicit "none"/"off" to disable a
// whole asset class.
func parseList(value string) []string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "none", "off", "-":
		return nil
	default:
		return splitList(value)
	}
}

func (c *Config) normalize() {
	c.Sources = dedupeLower(c.Sources)
	c.Crypto = dedupeUpper(c.Crypto)
	fiat := make([]string, 0, len(c.Fiat))
	for _, f := range c.Fiat {
		f = strings.TrimSpace(f)
		if f == "*" {
			fiat = append(fiat, f)
			continue
		}
		fiat = append(fiat, strings.ToUpper(f))
	}
	c.Fiat = dedupe(fiat)
	c.Strategy = normalizeStrategy(c.Strategy)
	c.StrategyFiat = normalizeStrategy(c.StrategyFiat)
	c.StrategyCrypto = normalizeStrategy(c.StrategyCrypto)
	if c.Strategy == "" {
		c.Strategy = model.StrategyPriority
	}
	if c.StrategyFiat == "" {
		c.StrategyFiat = c.Strategy
	}
	if c.StrategyCrypto == "" {
		c.StrategyCrypto = c.Strategy
	}
	c.LogFormat = strings.ToLower(strings.TrimSpace(c.LogFormat))
	c.LogLevel = strings.ToLower(strings.TrimSpace(c.LogLevel))
}

// Validate checks the configuration for obviously broken values.
func (c Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", c.Port)
	}
	if len(c.Sources) == 0 {
		return fmt.Errorf("at least one source is required")
	}
	for _, s := range c.Sources {
		if !knownSources[s] {
			return fmt.Errorf("unknown source %q (known: %s)", s, strings.Join(knownSourceList(), ", "))
		}
	}
	strategies := []struct {
		name  string
		value model.Strategy
	}{
		{"strategy", c.Strategy},
		{"strategy_fiat", c.StrategyFiat},
		{"strategy_crypto", c.StrategyCrypto},
	}
	for _, s := range strategies {
		if !s.value.Valid() {
			return fmt.Errorf("%s must be one of priority, max, min, avg, median; got %q", s.name, s.value)
		}
	}
	if len(c.Fiat) == 0 && len(c.Crypto) == 0 {
		return fmt.Errorf("at least one fiat or crypto asset must be enabled")
	}
	durations := map[string]time.Duration{
		"poll_market":      c.PollMarket,
		"poll_tcmb":        c.PollTCMB,
		"poll_fiat":        c.PollFiat,
		"poll_coingecko":   c.PollCoinGecko,
		"poll_fawaz":       c.PollFawaz,
		"stale_market":     c.StaleMarket,
		"stale_official":   c.StaleOfficial,
		"stale_aggregator": c.StaleAggregator,
		"startup_timeout":  c.StartupTimeout,
	}
	for name, d := range durations {
		if d <= 0 {
			return fmt.Errorf("%s must be positive", name)
		}
	}
	switch c.LogFormat {
	case "json", "text":
	default:
		return fmt.Errorf("log_format must be json or text, got %q", c.LogFormat)
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log_level must be debug, info, warn or error, got %q", c.LogLevel)
	}
	return nil
}

// normalizeStrategy canonicalizes strategy names and accepts a few natural
// aliases so RATE_STRATEGY=highest works as expected.
func normalizeStrategy(s model.Strategy) model.Strategy {
	switch strings.ToLower(strings.TrimSpace(string(s))) {
	case "highest", "high", "maximum":
		return model.StrategyMax
	case "lowest", "low", "minimum":
		return model.StrategyMin
	case "average", "mean":
		return model.StrategyAvg
	case "":
		return ""
	default:
		return model.Strategy(strings.ToLower(strings.TrimSpace(string(s))))
	}
}

func knownSourceList() []string {
	out := make([]string, 0, len(knownSources))
	for k := range knownSources {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func dedupeLower(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func dedupeUpper(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.ToUpper(strings.TrimSpace(s))
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
