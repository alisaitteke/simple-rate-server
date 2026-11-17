package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"syscall"
	"time"

	"github.com/alisaitteke/simple-rate-server/internal/assets"
	"github.com/alisaitteke/simple-rate-server/internal/config"
	"github.com/alisaitteke/simple-rate-server/internal/engine"
	"github.com/alisaitteke/simple-rate-server/internal/httpapi"
	"github.com/alisaitteke/simple-rate-server/internal/model"
	"github.com/alisaitteke/simple-rate-server/internal/provider"
	"github.com/alisaitteke/simple-rate-server/internal/provider/binance"
	"github.com/alisaitteke/simple-rate-server/internal/provider/coinbase"
	"github.com/alisaitteke/simple-rate-server/internal/provider/coingecko"
	"github.com/alisaitteke/simple-rate-server/internal/provider/ecb"
	"github.com/alisaitteke/simple-rate-server/internal/provider/erapi"
	"github.com/alisaitteke/simple-rate-server/internal/provider/fawazahmed"
	"github.com/alisaitteke/simple-rate-server/internal/provider/tcmb"
	"github.com/alisaitteke/simple-rate-server/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		configPath = flag.String("config", "", "path to YAML config file")
		port       = flag.Int("port", 0, "override HTTP port")
		bind       = flag.String("bind", "", "override bind address")
	)
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if *port != 0 {
		cfg.Port = *port
	}
	if *bind != "" {
		cfg.Bind = *bind
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	logger := newLogger(cfg)
	reg, err := assets.New()
	if err != nil {
		return err
	}
	enabledFiat, enabledCrypto, err := enabledSets(reg, cfg)
	if err != nil {
		return err
	}

	client := provider.NewHTTPClient(15*time.Second, version.UserAgent())
	providers := buildProviders(cfg, client, enabledFiat, enabledCrypto, logger)

	eng := engine.New(engine.Options{
		Registry:        reg,
		EnabledFiat:     enabledFiat,
		EnabledCrypto:   enabledCrypto,
		StaleMarket:     cfg.StaleMarket,
		StaleOfficial:   cfg.StaleOfficial,
		StaleAggregator: cfg.StaleAggregator,
		StrategyFiat:    cfg.StrategyFiat,
		StrategyCrypto:  cfg.StrategyCrypto,
		Logger:          logger,
	}, providers)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("starting rate-server",
		"version", version.Version,
		"sources", cfg.Sources,
		"fiat_assets", len(enabledFiat),
		"crypto_assets", len(enabledCrypto),
		"strategy_fiat", cfg.StrategyFiat,
		"strategy_crypto", cfg.StrategyCrypto,
	)

	srv := httpapi.New(eng, cfg, logger)
	addr := net.JoinHostPort(cfg.Bind, strconv.Itoa(cfg.Port))
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	logger.Info("listening", "addr", ln.Addr().String())

	// Fetch upstreams in the background so the port is reachable immediately.
	// /readyz reports 200 as soon as the first provider has delivered rates.
	go eng.Start(ctx, cfg.StartupTimeout)

	errCh := make(chan error, 1)
	go func() {
		if err := httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutting down")
	case err := <-errCh:
		return err
	}
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}

func newLogger(cfg config.Config) *slog.Logger {
	level := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.LogFormat == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

func enabledSets(reg *assets.Registry, cfg config.Config) (map[string]bool, map[string]bool, error) {
	fiat := make(map[string]bool)
	allFiat := false
	for _, f := range cfg.Fiat {
		if f == "*" {
			allFiat = true
			break
		}
	}
	if allFiat {
		for _, a := range reg.List(model.KindFiat) {
			fiat[a.Code] = true
		}
	} else {
		for _, code := range cfg.Fiat {
			norm, ok := reg.Normalize(code)
			if !ok {
				return nil, nil, fmt.Errorf("unknown fiat asset %q", code)
			}
			a, _ := reg.Get(norm)
			if a.Kind != model.KindFiat {
				return nil, nil, fmt.Errorf("%s is not a fiat currency", code)
			}
			fiat[norm] = true
		}
	}

	crypto := make(map[string]bool)
	for _, code := range cfg.Crypto {
		norm, ok := reg.Normalize(code)
		if !ok {
			return nil, nil, fmt.Errorf("unknown crypto asset %q", code)
		}
		a, _ := reg.Get(norm)
		if a.Kind != model.KindCrypto {
			return nil, nil, fmt.Errorf("%s is not a crypto asset", code)
		}
		crypto[norm] = true
	}
	if len(fiat) == 0 && len(crypto) == 0 {
		return nil, nil, fmt.Errorf("no assets enabled")
	}
	return fiat, crypto, nil
}

func buildProviders(cfg config.Config, client *provider.HTTPClient, fiat, crypto map[string]bool, log *slog.Logger) []provider.Provider {
	cryptoCodes := sortedKeys(crypto)
	fiatCodes := sortedKeys(fiat)
	quotes := append(append([]string{}, cryptoCodes...), fiatCodes...)

	out := make([]provider.Provider, 0, len(cfg.Sources))
	for _, name := range cfg.Sources {
		switch name {
		case "ecb":
			out = append(out, ecb.New(cfg.PollFiat, client))
		case "erapi":
			out = append(out, erapi.New(cfg.PollFiat, client))
		case "tcmb":
			out = append(out, tcmb.New(cfg.PollTCMB, client))
		case "binance":
			if len(cryptoCodes) == 0 {
				log.Warn("binance skipped: no crypto assets enabled")
				continue
			}
			out = append(out, binance.New(cfg.PollMarket, client, cryptoCodes, quotes))
		case "coinbase":
			out = append(out, coinbase.New(cfg.PollMarket, client))
		case "coingecko":
			if len(cryptoCodes) == 0 {
				log.Warn("coingecko skipped: no crypto assets enabled")
				continue
			}
			out = append(out, coingecko.New(cfg.PollCoinGecko, client, cryptoCodes))
		case "fawazahmed":
			out = append(out, fawazahmed.New(cfg.PollFawaz, client, quotes))
		}
	}
	if len(out) == 0 {
		log.Warn("no providers registered")
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k, ok := range m {
		if ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
