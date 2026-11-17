// Package erapi fetches the keyless open exchange rates endpoint from
// exchangerate-api.com.
package erapi

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/alisaitteke/simple-rate-server/internal/model"
	"github.com/alisaitteke/simple-rate-server/internal/provider"
)

// DefaultURL is the free, keyless USD-based endpoint.
const DefaultURL = "https://open.er-api.com/v6/latest/USD"

// Provider implements provider.Provider for open.er-api.com.
type Provider struct {
	interval time.Duration
	client   *provider.HTTPClient
	URL      string
}

// New builds an er-api provider.
func New(interval time.Duration, client *provider.HTTPClient) *Provider {
	return &Provider{interval: interval, client: client, URL: DefaultURL}
}

func (p *Provider) Name() string            { return "erapi" }
func (p *Provider) Class() model.Class      { return model.ClassAggregator }
func (p *Provider) Interval() time.Duration { return p.interval }

type response struct {
	Result             string             `json:"result"`
	BaseCode           string             `json:"base_code"`
	TimeLastUpdateUnix int64              `json:"time_last_update_unix"`
	Rates              map[string]float64 `json:"rates"`
}

// Fetch downloads the latest USD-based rates.
func (p *Provider) Fetch(ctx context.Context) ([]model.Edge, error) {
	body, err := p.client.Get(ctx, p.URL)
	if err != nil {
		return nil, err
	}
	var res response
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("erapi: parse json: %w", err)
	}
	if res.Result != "" && res.Result != "success" {
		return nil, fmt.Errorf("erapi: upstream result %q", res.Result)
	}
	base := res.BaseCode
	if base == "" {
		base = "USD"
	}
	date := time.Now().UTC().Truncate(24 * time.Hour)
	if res.TimeLastUpdateUnix > 0 {
		date = time.Unix(res.TimeLastUpdateUnix, 0).UTC().Truncate(24 * time.Hour)
	}
	now := time.Now().UTC()
	edges := make([]model.Edge, 0, len(res.Rates))
	for code, v := range res.Rates {
		if code == "" || code == base || v <= 0 {
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
		return nil, fmt.Errorf("erapi: no rates parsed")
	}
	return edges, nil
}
