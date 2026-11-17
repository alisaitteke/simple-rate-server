// Package coinbase fetches live rates from Coinbase's public exchange-rates
// endpoint. One request returns crypto and fiat quotes against USD.
package coinbase

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/alisaitteke/simple-rate-server/internal/model"
	"github.com/alisaitteke/simple-rate-server/internal/provider"
)

// DefaultURL returns every rate with USD as base.
const DefaultURL = "https://api.coinbase.com/v2/exchange-rates?currency=USD"

// Provider implements provider.Provider for Coinbase.
type Provider struct {
	interval time.Duration
	client   *provider.HTTPClient
	URL      string
}

// New builds a Coinbase provider.
func New(interval time.Duration, client *provider.HTTPClient) *Provider {
	return &Provider{interval: interval, client: client, URL: DefaultURL}
}

func (p *Provider) Name() string            { return "coinbase" }
func (p *Provider) Class() model.Class      { return model.ClassMarket }
func (p *Provider) Interval() time.Duration { return p.interval }

type response struct {
	Data struct {
		Currency string            `json:"currency"`
		Rates    map[string]string `json:"rates"`
	} `json:"data"`
}

// Fetch downloads Coinbase's USD-based rate table.
func (p *Provider) Fetch(ctx context.Context) ([]model.Edge, error) {
	body, err := p.client.Get(ctx, p.URL)
	if err != nil {
		return nil, err
	}
	var res response
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("coinbase: parse json: %w", err)
	}
	base := res.Data.Currency
	if base == "" {
		base = "USD"
	}
	now := time.Now().UTC()
	date := now.Truncate(24 * time.Hour)
	edges := make([]model.Edge, 0, len(res.Data.Rates))
	for code, raw := range res.Data.Rates {
		if code == "" || code == base {
			continue
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || v <= 0 {
			continue
		}
		edges = append(edges, model.Edge{
			From:      base,
			To:        code,
			Rate:      v,
			Source:    p.Name(),
			Class:     p.Class(),
			RateDate:  date,
			FetchedAt: now,
		})
	}
	if len(edges) == 0 {
		return nil, fmt.Errorf("coinbase: no rates parsed")
	}
	return edges, nil
}
