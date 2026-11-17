// Package model contains the shared data types used by providers, the
// resolution engine and the HTTP API.
package model

import "time"

// Kind distinguishes fiat currencies from crypto assets.
type Kind string

const (
	KindFiat   Kind = "fiat"
	KindCrypto Kind = "crypto"
)

// Valid reports whether k is a supported asset kind.
func (k Kind) Valid() bool { return k == KindFiat || k == KindCrypto }

// Class describes where a quote comes from.
type Class string

const (
	// ClassOfficial is a central bank or other official reference rate.
	ClassOfficial Class = "official"
	// ClassMarket is a live venue quote.
	ClassMarket Class = "market"
	// ClassAggregator is a third-party API that aggregates other sources.
	ClassAggregator Class = "aggregator"
)

// Strategy selects how multiple quotes for the same pair are combined.
type Strategy string

const (
	// StrategyPriority picks the highest-priority source.
	StrategyPriority Strategy = "priority"
	// StrategyMax picks the highest quote in the requested direction.
	StrategyMax Strategy = "max"
	// StrategyMin picks the lowest quote in the requested direction.
	StrategyMin Strategy = "min"
	// StrategyAvg takes the arithmetic mean of all source quotes.
	StrategyAvg Strategy = "avg"
	// StrategyMedian takes the median of all source quotes.
	StrategyMedian Strategy = "median"
)

// Valid reports whether s is a supported aggregation strategy.
func (s Strategy) Valid() bool {
	switch s {
	case StrategyPriority, StrategyMax, StrategyMin, StrategyAvg, StrategyMedian:
		return true
	}
	return false
}

// Asset is a fiat currency or crypto asset understood by the server.
type Asset struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Kind     Kind   `json:"kind"`
	Decimals int    `json:"decimals"`
}

// Edge is a directed quote: 1 unit of From equals Rate units of To.
type Edge struct {
	From      string
	To        string
	Rate      float64
	Source    string
	Class     Class
	RateDate  time.Time // bulletin date for official sources, day of the quote for market sources
	FetchedAt time.Time // when this server received the quote
	// Contributors lists the sources behind an aggregated quote. It is empty
	// for priority-selected and single-source quotes.
	Contributors []string
}

// Inverse returns the reverse quote (To -> From).
func (e Edge) Inverse() Edge {
	return Edge{
		From:         e.To,
		To:           e.From,
		Rate:         1 / e.Rate,
		Source:       e.Source,
		Class:        e.Class,
		RateDate:     e.RateDate,
		FetchedAt:    e.FetchedAt,
		Contributors: e.Contributors,
	}
}

// Resolved is a pair resolved through the currency graph.
type Resolved struct {
	From     string
	To       string
	Rate     float64
	Path     []string
	Edges    []Edge
	AsOf     time.Time // oldest fetch time on the path
	RateDate time.Time // oldest bulletin date on the path
	Stale    bool
	Source   string // source of the first edge on the path
}
