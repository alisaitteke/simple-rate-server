// Package binance fetches live crypto prices from Binance's public API.
package binance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alisaitteke/simple-rate-server/internal/model"
	"github.com/alisaitteke/simple-rate-server/internal/provider"
)

const (
	// DefaultExchangeInfoURL lists tradable symbols.
	DefaultExchangeInfoURL = "https://api.binance.com/api/v3/exchangeInfo"
	// DefaultTickerURL returns latest prices.
	DefaultTickerURL = "https://api.binance.com/api/v3/ticker/price"
	// symbolTTL is how long the symbol catalogue is reused.
	symbolTTL = 24 * time.Hour
)

// Provider implements provider.Provider for Binance spot markets.
type Provider struct {
	interval        time.Duration
	client          *provider.HTTPClient
	ExchangeInfoURL string
	TickerURL       string

	baseAssets  map[string]bool
	quoteAssets map[string]bool

	mu        sync.Mutex
	symbols   []string
	pairs     map[string][2]string
	fetchedAt time.Time
}

// New builds a Binance provider. bases are the crypto assets we quote,
// quotes are the assets they may be quoted against (crypto + fiat).
func New(interval time.Duration, client *provider.HTTPClient, bases, quotes []string) *Provider {
	p := &Provider{
		interval:        interval,
		client:          client,
		ExchangeInfoURL: DefaultExchangeInfoURL,
		TickerURL:       DefaultTickerURL,
		baseAssets:      make(map[string]bool, len(bases)),
		quoteAssets:     make(map[string]bool, len(quotes)),
		pairs:           map[string][2]string{},
	}
	for _, b := range bases {
		if b = strings.ToUpper(strings.TrimSpace(b)); b != "" {
			p.baseAssets[b] = true
		}
	}
	for _, q := range quotes {
		if q = strings.ToUpper(strings.TrimSpace(q)); q != "" {
			p.quoteAssets[q] = true
		}
	}
	return p
}

func (p *Provider) Name() string            { return "binance" }
func (p *Provider) Class() model.Class      { return model.ClassMarket }
func (p *Provider) Interval() time.Duration { return p.interval }

type ticker struct {
	Symbol string `json:"symbol"`
	Price  string `json:"price"`
}

// Fetch returns live Binance quotes. It first asks for only the symbols that
// match the configured assets, falling back to the full ticker list.
func (p *Provider) Fetch(ctx context.Context) ([]model.Edge, error) {
	if err := p.ensureSymbols(ctx); err != nil {
		if isRetryAfter(err) {
			return nil, err
		}
		return p.fetchAll(ctx)
	}
	p.mu.Lock()
	symbols := append([]string(nil), p.symbols...)
	pairs := make(map[string][2]string, len(p.pairs))
	for k, v := range p.pairs {
		pairs[k] = v
	}
	p.mu.Unlock()

	payload, err := json.Marshal(symbols)
	if err != nil {
		return nil, err
	}
	body, err := p.client.Get(ctx, p.TickerURL+"?symbols="+url.QueryEscape(string(payload)))
	if err != nil {
		if isRetryAfter(err) {
			return nil, err
		}
		p.invalidateSymbols()
		return p.fetchAll(ctx)
	}
	var tickers []ticker
	if err := json.Unmarshal(body, &tickers); err != nil {
		return nil, fmt.Errorf("binance: parse tickers: %w", err)
	}
	edges := p.toEdges(tickers, pairs)
	if len(edges) == 0 {
		return nil, fmt.Errorf("binance: no prices parsed")
	}
	return edges, nil
}

func (p *Provider) ensureSymbols(ctx context.Context) error {
	p.mu.Lock()
	fresh := len(p.symbols) > 0 && time.Since(p.fetchedAt) < symbolTTL
	p.mu.Unlock()
	if fresh {
		return nil
	}

	body, err := p.client.Get(ctx, p.ExchangeInfoURL)
	if err != nil {
		return err
	}
	var info struct {
		Symbols []struct {
			Symbol     string `json:"symbol"`
			Status     string `json:"status"`
			BaseAsset  string `json:"baseAsset"`
			QuoteAsset string `json:"quoteAsset"`
		} `json:"symbols"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return fmt.Errorf("binance: parse exchangeInfo: %w", err)
	}

	symbols := make([]string, 0, len(info.Symbols))
	pairs := make(map[string][2]string, len(info.Symbols))
	for _, s := range info.Symbols {
		base := strings.ToUpper(s.BaseAsset)
		quote := strings.ToUpper(s.QuoteAsset)
		if s.Status != "TRADING" || base == quote {
			continue
		}
		if !p.baseAssets[base] || !p.quoteAssets[quote] {
			continue
		}
		symbols = append(symbols, s.Symbol)
		pairs[s.Symbol] = [2]string{base, quote}
	}
	if len(symbols) == 0 {
		return fmt.Errorf("binance: no configured symbols are tradable")
	}
	sort.Strings(symbols)

	p.mu.Lock()
	p.symbols = symbols
	p.pairs = pairs
	p.fetchedAt = time.Now()
	p.mu.Unlock()
	return nil
}

// fetchAll is the degraded path: download every ticker and match symbols
// against the configured asset sets.
func (p *Provider) fetchAll(ctx context.Context) ([]model.Edge, error) {
	body, err := p.client.Get(ctx, p.TickerURL)
	if err != nil {
		return nil, err
	}
	var tickers []ticker
	if err := json.Unmarshal(body, &tickers); err != nil {
		return nil, fmt.Errorf("binance: parse all tickers: %w", err)
	}
	quotes := make([]string, 0, len(p.quoteAssets))
	for q := range p.quoteAssets {
		quotes = append(quotes, q)
	}
	sort.Slice(quotes, func(i, j int) bool { return len(quotes[i]) > len(quotes[j]) })

	pairs := make(map[string][2]string, len(tickers))
	for _, t := range tickers {
		base, quote, ok := p.splitSymbol(t.Symbol, quotes)
		if ok {
			pairs[t.Symbol] = [2]string{base, quote}
		}
	}
	edges := p.toEdges(tickers, pairs)
	if len(edges) == 0 {
		return nil, fmt.Errorf("binance: no prices parsed")
	}
	return edges, nil
}

func (p *Provider) splitSymbol(symbol string, quotes []string) (string, string, bool) {
	for _, q := range quotes {
		if !strings.HasSuffix(symbol, q) {
			continue
		}
		base := strings.TrimSuffix(symbol, q)
		if p.baseAssets[base] {
			return base, q, true
		}
	}
	return "", "", false
}

func (p *Provider) toEdges(tickers []ticker, pairs map[string][2]string) []model.Edge {
	now := time.Now().UTC()
	date := now.Truncate(24 * time.Hour)
	edges := make([]model.Edge, 0, len(tickers))
	for _, t := range tickers {
		pair, ok := pairs[t.Symbol]
		if !ok {
			continue
		}
		rate, err := strconv.ParseFloat(t.Price, 64)
		if err != nil || rate <= 0 {
			continue
		}
		edges = append(edges, model.Edge{
			From:      pair[0],
			To:        pair[1],
			Rate:      rate,
			Source:    p.Name(),
			Class:     p.Class(),
			RateDate:  date,
			FetchedAt: now,
		})
	}
	return edges
}

func (p *Provider) invalidateSymbols() {
	p.mu.Lock()
	p.fetchedAt = time.Time{}
	p.mu.Unlock()
}

func isRetryAfter(err error) bool {
	var ra *provider.RetryAfterError
	return errors.As(err, &ra)
}
