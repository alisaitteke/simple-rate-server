// Package ecb fetches the European Central Bank daily reference rates.
package ecb

import (
	"context"
	"encoding/xml"
	"fmt"
	"strconv"
	"time"

	"github.com/alisaitteke/simple-rate-server/internal/model"
	"github.com/alisaitteke/simple-rate-server/internal/provider"
)

// DefaultURL is the ECB daily reference rate feed.
const DefaultURL = "https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml"

// Provider implements provider.Provider for the ECB feed.
type Provider struct {
	interval time.Duration
	client   *provider.HTTPClient
	URL      string
}

// New builds an ECB provider.
func New(interval time.Duration, client *provider.HTTPClient) *Provider {
	return &Provider{interval: interval, client: client, URL: DefaultURL}
}

func (p *Provider) Name() string            { return "ecb" }
func (p *Provider) Class() model.Class      { return model.ClassOfficial }
func (p *Provider) Interval() time.Duration { return p.interval }

type envelope struct {
	Cube struct {
		Days []day `xml:"Cube"`
	} `xml:"Cube"`
}

type day struct {
	Time  string `xml:"time,attr"`
	Rates []rate `xml:"Cube"`
}

type rate struct {
	Currency string `xml:"currency,attr"`
	Rate     string `xml:"rate,attr"`
}

// Fetch downloads and parses the daily ECB bulletin.
func (p *Provider) Fetch(ctx context.Context) ([]model.Edge, error) {
	body, err := p.client.Get(ctx, p.URL)
	if err != nil {
		return nil, err
	}
	var env envelope
	if err := xml.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("ecb: parse xml: %w", err)
	}
	now := time.Now().UTC()
	edges := make([]model.Edge, 0, 40)
	for _, d := range env.Cube.Days {
		date, err := time.Parse("2006-01-02", d.Time)
		if err != nil {
			continue
		}
		for _, r := range d.Rates {
			v, err := strconv.ParseFloat(r.Rate, 64)
			if err != nil || v <= 0 || r.Currency == "" {
				continue
			}
			edges = append(edges, model.Edge{
				From:      "EUR",
				To:        r.Currency,
				Rate:      v,
				Source:    p.Name(),
				Class:     p.Class(),
				RateDate:  date,
				FetchedAt: now,
			})
		}
	}
	if len(edges) == 0 {
		return nil, fmt.Errorf("ecb: no rates parsed")
	}
	return edges, nil
}
