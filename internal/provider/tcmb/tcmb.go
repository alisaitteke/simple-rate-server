// Package tcmb fetches the Central Bank of the Republic of Turkey daily
// exchange rate bulletin (keyless XML).
package tcmb

import (
	"context"
	"encoding/xml"
	"fmt"
	"strconv"
	"time"

	"github.com/alisaitteke/simple-rate-server/internal/model"
	"github.com/alisaitteke/simple-rate-server/internal/provider"
)

// DefaultURL is TCMB's bulletin for the current business day.
const DefaultURL = "https://www.tcmb.gov.tr/kurlar/today.xml"

// Provider implements provider.Provider for TCMB.
type Provider struct {
	interval time.Duration
	client   *provider.HTTPClient
	URL      string
}

// New builds a TCMB provider.
func New(interval time.Duration, client *provider.HTTPClient) *Provider {
	return &Provider{interval: interval, client: client, URL: DefaultURL}
}

func (p *Provider) Name() string            { return "tcmb" }
func (p *Provider) Class() model.Class      { return model.ClassOfficial }
func (p *Provider) Interval() time.Duration { return p.interval }

type envelope struct {
	Date       string `xml:"Date,attr"`
	Tarih      string `xml:"Tarih,attr"`
	Currencies []struct {
		Code         string `xml:"CurrencyCode,attr"`
		Unit         string `xml:"Unit"`
		ForexBuying  string `xml:"ForexBuying"`
		ForexSelling string `xml:"ForexSelling"`
	} `xml:"Currency"`
}

// Fetch downloads and parses today's TCMB bulletin. TCMB quotes TRY per Unit
// of foreign currency, so the edge is code -> TRY with rate mid/unit.
func (p *Provider) Fetch(ctx context.Context) ([]model.Edge, error) {
	body, err := p.client.Get(ctx, p.URL)
	if err != nil {
		return nil, err
	}
	var env envelope
	if err := xml.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("tcmb: parse xml: %w", err)
	}
	date := parseDate(env.Date, env.Tarih)
	now := time.Now().UTC()
	edges := make([]model.Edge, 0, len(env.Currencies))
	for _, c := range env.Currencies {
		code := c.Code
		if code == "" || code == "TRY" {
			continue
		}
		unit := 1.0
		if c.Unit != "" {
			if u, err := strconv.ParseFloat(c.Unit, 64); err == nil && u > 0 {
				unit = u
			}
		}
		buy, _ := strconv.ParseFloat(c.ForexBuying, 64)
		sell, _ := strconv.ParseFloat(c.ForexSelling, 64)
		mid := midRate(buy, sell)
		if mid <= 0 {
			continue
		}
		edges = append(edges, model.Edge{
			From:      code,
			To:        "TRY",
			Rate:      mid / unit,
			Source:    p.Name(),
			Class:     p.Class(),
			RateDate:  date,
			FetchedAt: now,
		})
	}
	if len(edges) == 0 {
		return nil, fmt.Errorf("tcmb: no rates parsed")
	}
	return edges, nil
}

func midRate(buy, sell float64) float64 {
	switch {
	case buy > 0 && sell > 0:
		return (buy + sell) / 2
	case buy > 0:
		return buy
	case sell > 0:
		return sell
	default:
		return 0
	}
}

func parseDate(date, tarih string) time.Time {
	if t, err := time.Parse("01/02/2006", date); err == nil {
		return t
	}
	if t, err := time.Parse("02.01.2006", tarih); err == nil {
		return t
	}
	return time.Now().UTC().Truncate(24 * time.Hour)
}
