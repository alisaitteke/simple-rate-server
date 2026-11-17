// Package fawazahmed fetches the daily currency dataset published by
// fawazahmed0, served from jsDelivr with a Cloudflare Pages fallback.
package fawazahmed

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/alisaitteke/simple-rate-server/internal/model"
	"github.com/alisaitteke/simple-rate-server/internal/provider"
)

// DefaultURLs are tried in order. They serve the same static dataset.
var DefaultURLs = []string{
	"https://cdn.jsdelivr.net/npm/@fawazahmed0/currency-api@latest/v1/currencies/usd.min.json",
	"https://latest.currency-api.pages.dev/v1/currencies/usd.min.json",
}

// Provider implements provider.Provider for the fawazahmed0 dataset.
type Provider struct {
	interval time.Duration
	client   *provider.HTTPClient
	URLs     []string
	enabled  map[string]bool
}

// New builds a fawazahmed provider for the enabled asset codes.
func New(interval time.Duration, client *provider.HTTPClient, codes []string) *Provider {
	p := &Provider{
		interval: interval,
		client:   client,
		URLs:     append([]string(nil), DefaultURLs...),
		enabled:  make(map[string]bool, len(codes)),
	}
	for _, c := range codes {
		if c = strings.ToUpper(strings.TrimSpace(c)); c != "" {
			p.enabled[c] = true
		}
	}
	return p
}

func (p *Provider) Name() string            { return "fawazahmed" }
func (p *Provider) Class() model.Class      { return model.ClassAggregator }
func (p *Provider) Interval() time.Duration { return p.interval }

// Fetch tries each mirror until one returns usable data.
func (p *Provider) Fetch(ctx context.Context) ([]model.Edge, error) {
	var lastErr error
	for _, u := range p.URLs {
		edges, err := p.fetchOne(ctx, u)
		if err == nil {
			return edges, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("fawazahmed: no mirrors configured")
	}
	return nil, lastErr
}

func (p *Provider) fetchOne(ctx context.Context, u string) ([]model.Edge, error) {
	body, err := p.client.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, fmt.Errorf("fawazahmed: parse json: %w", err)
	}
	rawRates, ok := top["usd"]
	if !ok {
		return nil, fmt.Errorf("fawazahmed: missing usd rates")
	}
	var rates map[string]float64
	if err := json.Unmarshal(rawRates, &rates); err != nil {
		return nil, fmt.Errorf("fawazahmed: parse usd rates: %w", err)
	}

	date := time.Now().UTC().Truncate(24 * time.Hour)
	var dateStr string
	if raw, ok := top["date"]; ok {
		_ = json.Unmarshal(raw, &dateStr)
	}
	if t, err := time.Parse("2006-01-02", dateStr); err == nil {
		date = t
	}

	now := time.Now().UTC()
	edges := make([]model.Edge, 0, len(rates))
	for code, v := range rates {
		up := strings.ToUpper(code)
		if !p.enabled[up] || v <= 0 {
			continue
		}
		edges = append(edges, model.Edge{
			From:      "USD",
			To:        up,
			Rate:      v,
			Source:    p.Name(),
			Class:     p.Class(),
			RateDate:  date,
			FetchedAt: now,
		})
	}
	if len(edges) == 0 {
		return nil, fmt.Errorf("fawazahmed: no enabled rates parsed")
	}
	return edges, nil
}
