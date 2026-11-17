// Package coingecko fetches crypto prices from CoinGecko's keyless API.
package coingecko

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/alisaitteke/simple-rate-server/internal/model"
	"github.com/alisaitteke/simple-rate-server/internal/provider"
)

// DefaultURL is the simple/price endpoint.
const DefaultURL = "https://api.coingecko.com/api/v3/simple/price"

// ids maps canonical asset codes to CoinGecko coin ids.
var ids = map[string]string{
	"BTC":  "bitcoin",
	"ETH":  "ethereum",
	"USDT": "tether",
	"USDC": "usd-coin",
	"SOL":  "solana",
	"XRP":  "ripple",
	"ADA":  "cardano",
	"DOGE": "dogecoin",
	"AVAX": "avalanche-2",
	"DOT":  "polkadot",
	"BNB":  "binancecoin",
	"LTC":  "litecoin",
}

var vsCurrencies = []string{"usd", "eur", "try"}

// Provider implements provider.Provider for CoinGecko.
type Provider struct {
	interval time.Duration
	client   *provider.HTTPClient
	URL      string
	byID     map[string]string
}

// New builds a CoinGecko provider for the enabled crypto codes.
func New(interval time.Duration, client *provider.HTTPClient, codes []string) *Provider {
	p := &Provider{
		interval: interval,
		client:   client,
		URL:      DefaultURL,
		byID:     make(map[string]string, len(codes)),
	}
	for _, code := range codes {
		code = strings.ToUpper(strings.TrimSpace(code))
		if id, ok := ids[code]; ok {
			p.byID[id] = code
		}
	}
	return p
}

func (p *Provider) Name() string            { return "coingecko" }
func (p *Provider) Class() model.Class      { return model.ClassAggregator }
func (p *Provider) Interval() time.Duration { return p.interval }

// Fetch downloads all configured coins in one request.
func (p *Provider) Fetch(ctx context.Context) ([]model.Edge, error) {
	if len(p.byID) == 0 {
		return nil, fmt.Errorf("coingecko: no supported crypto assets enabled")
	}
	idList := make([]string, 0, len(p.byID))
	for id := range p.byID {
		idList = append(idList, id)
	}
	u := p.URL + "?ids=" + url.QueryEscape(strings.Join(idList, ",")) +
		"&vs_currencies=" + strings.Join(vsCurrencies, ",")
	body, err := p.client.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	var res map[string]map[string]float64
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("coingecko: parse json: %w", err)
	}
	now := time.Now().UTC()
	date := now.Truncate(24 * time.Hour)
	edges := make([]model.Edge, 0, len(res)*len(vsCurrencies))
	for id, quotes := range res {
		code, ok := p.byID[id]
		if !ok {
			continue
		}
		for vs, v := range quotes {
			if v <= 0 {
				continue
			}
			edges = append(edges, model.Edge{
				From:      code,
				To:        strings.ToUpper(vs),
				Rate:      v,
				Source:    p.Name(),
				Class:     p.Class(),
				RateDate:  date,
				FetchedAt: now,
			})
		}
	}
	if len(edges) == 0 {
		return nil, fmt.Errorf("coingecko: no rates parsed")
	}
	return edges, nil
}
