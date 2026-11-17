package engine

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/alisaitteke/simple-rate-server/internal/model"
)

func we(from, to string, rate float64, source string, class model.Class, rank int, at time.Time) weightedEdge {
	return weightedEdge{
		edge: model.Edge{
			From: from, To: to, Rate: rate,
			Source: source, Class: class,
			RateDate: at, FetchedAt: at,
		},
		rank: rank,
	}
}

func testStale(c model.Class) time.Duration {
	if c == model.ClassMarket {
		return 5 * time.Minute
	}
	return 96 * time.Hour
}

func newTestState(t *testing.T, edges []weightedEdge, now time.Time) *State {
	t.Helper()
	return newState(1, now, map[string]model.Asset{}, edges, testStale, func(_, _ string) model.Strategy {
		return model.StrategyPriority
	})
}

func newTestStateWithStrategy(t *testing.T, edges []weightedEdge, now time.Time, strategy model.Strategy) *State {
	t.Helper()
	return newState(1, now, map[string]model.Asset{}, edges, testStale, func(_, _ string) model.Strategy {
		return strategy
	})
}

func almost(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestPreferOfficialAtSameHops(t *testing.T) {
	now := time.Now().UTC()
	edges := []weightedEdge{
		we("USD", "EUR", 0.90, "ecb", model.ClassOfficial, 1, now),
		we("USD", "EUR", 0.91, "coinbase", model.ClassMarket, 3, now),
	}
	s := newTestState(t, edges, now)

	res, ok := s.Resolve("USD", "EUR", now)
	if !ok {
		t.Fatal("expected a path")
	}
	if !almost(res.Rate, 0.90) {
		t.Fatalf("rate = %v, want 0.90 (official source must win)", res.Rate)
	}
	if res.Source != "ecb" {
		t.Fatalf("source = %q, want ecb", res.Source)
	}
	if !reflect.DeepEqual(res.Path, []string{"USD", "EUR"}) {
		t.Fatalf("path = %v", res.Path)
	}
}

func TestInverseIsConsistent(t *testing.T) {
	now := time.Now().UTC()
	s := newTestState(t, []weightedEdge{
		we("USD", "EUR", 0.90, "ecb", model.ClassOfficial, 1, now),
	}, now)

	forward, ok := s.Resolve("USD", "EUR", now)
	if !ok {
		t.Fatal("expected USD/EUR")
	}
	inverse, ok := s.Resolve("EUR", "USD", now)
	if !ok {
		t.Fatal("expected EUR/USD")
	}
	if !almost(forward.Rate*inverse.Rate, 1) {
		t.Fatalf("forward*inverse = %v, want 1", forward.Rate*inverse.Rate)
	}
}

func TestBridgeThroughStablecoin(t *testing.T) {
	now := time.Now().UTC()
	s := newTestState(t, []weightedEdge{
		we("BTC", "USDT", 65000, "binance", model.ClassMarket, 0, now),
		we("USDT", "USD", 0.999, "coinbase", model.ClassMarket, 1, now),
	}, now)

	res, ok := s.Resolve("BTC", "USD", now)
	if !ok {
		t.Fatal("expected BTC/USD via USDT")
	}
	if !reflect.DeepEqual(res.Path, []string{"BTC", "USDT", "USD"}) {
		t.Fatalf("path = %v", res.Path)
	}
	if !almost(res.Rate, 65000*0.999) {
		t.Fatalf("rate = %v, want %v", res.Rate, 65000*0.999)
	}
}

func TestDirectBeatsTwoHops(t *testing.T) {
	now := time.Now().UTC()
	edges := []weightedEdge{
		we("BTC", "USDT", 65000, "binance", model.ClassMarket, 0, now),
		we("USDT", "TRY", 50.4, "binance", model.ClassMarket, 0, now),
		we("BTC", "TRY", 3270000, "binance", model.ClassMarket, 0, now),
	}
	s := newTestState(t, edges, now)

	res, ok := s.Resolve("BTC", "TRY", now)
	if !ok {
		t.Fatal("expected BTC/TRY")
	}
	if !reflect.DeepEqual(res.Path, []string{"BTC", "TRY"}) {
		t.Fatalf("path = %v, want direct BTC/TRY", res.Path)
	}
	if !almost(res.Rate, 3270000) {
		t.Fatalf("rate = %v", res.Rate)
	}
}

func TestTwoHopWhenNoDirect(t *testing.T) {
	now := time.Now().UTC()
	s := newTestState(t, []weightedEdge{
		we("BTC", "USDT", 65000, "binance", model.ClassMarket, 0, now),
		we("USDT", "TRY", 50.4, "binance", model.ClassMarket, 0, now),
	}, now)

	res, ok := s.Resolve("BTC", "TRY", now)
	if !ok {
		t.Fatal("expected BTC/TRY via USDT")
	}
	if !reflect.DeepEqual(res.Path, []string{"BTC", "USDT", "TRY"}) {
		t.Fatalf("path = %v", res.Path)
	}
	if !almost(res.Rate, 65000*50.4) {
		t.Fatalf("rate = %v", res.Rate)
	}
}

func TestNoPath(t *testing.T) {
	now := time.Now().UTC()
	s := newTestState(t, []weightedEdge{
		we("USD", "EUR", 0.90, "ecb", model.ClassOfficial, 1, now),
	}, now)
	if _, ok := s.Resolve("USD", "GBP", now); ok {
		t.Fatal("expected no path to GBP")
	}
}

func TestMaxHops(t *testing.T) {
	now := time.Now().UTC()
	edges := []weightedEdge{
		we("USD", "EUR", 1.1, "x", model.ClassOfficial, 0, now),
		we("EUR", "TRY", 10, "x", model.ClassOfficial, 0, now),
		we("TRY", "GBP", 0.02, "x", model.ClassOfficial, 0, now),
		we("GBP", "JPY", 200, "x", model.ClassOfficial, 0, now),
		we("JPY", "CHF", 0.006, "x", model.ClassOfficial, 0, now),
	}
	s := newTestState(t, edges, now)

	if _, ok := s.Resolve("USD", "JPY", now); !ok {
		t.Fatal("USD/JPY is 4 hops and should resolve")
	}
	if _, ok := s.Resolve("USD", "CHF", now); ok {
		t.Fatal("USD/CHF is 5 hops and must be rejected")
	}
}

func TestStaleMarketEdge(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-10 * time.Minute)
	s := newTestState(t, []weightedEdge{
		we("BTC", "USDT", 65000, "binance", model.ClassMarket, 0, old),
	}, now)

	res, ok := s.Resolve("BTC", "USDT", now)
	if !ok {
		t.Fatal("expected a path")
	}
	if !res.Stale {
		t.Fatal("10 minute old market quote must be stale")
	}
}

func TestRespectsRateDate(t *testing.T) {
	now := time.Now().UTC()
	older := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	s := newTestState(t, []weightedEdge{
		we("USD", "EUR", 0.90, "ecb", model.ClassOfficial, 1, now),
		we("EUR", "TRY", 55, "tcmb", model.ClassOfficial, 0, now),
	}, now)
	s.adj["USD"]["EUR"] = weightedEdge{edge: model.Edge{
		From: "USD", To: "EUR", Rate: 0.90, Source: "ecb",
		Class: model.ClassOfficial, RateDate: older, FetchedAt: now,
	}, rank: 1}
	s.adj["EUR"]["TRY"] = weightedEdge{edge: model.Edge{
		From: "EUR", To: "TRY", Rate: 55, Source: "tcmb",
		Class: model.ClassOfficial, RateDate: newer, FetchedAt: now,
	}, rank: 0}

	res, ok := s.Resolve("USD", "TRY", now)
	if !ok {
		t.Fatal("expected a path")
	}
	if !res.RateDate.Equal(older) {
		t.Fatalf("rate date = %v, want oldest edge date %v", res.RateDate, older)
	}
}

func TestMaxStrategy(t *testing.T) {
	now := time.Now().UTC()
	edges := []weightedEdge{
		we("USD", "EUR", 0.90, "ecb", model.ClassOfficial, 1, now),
		we("USD", "EUR", 0.91, "coinbase", model.ClassMarket, 3, now),
	}
	s := newTestStateWithStrategy(t, edges, now, model.StrategyMax)

	res, ok := s.Resolve("USD", "EUR", now)
	if !ok {
		t.Fatal("expected USD/EUR")
	}
	if !almost(res.Rate, 0.91) || res.Source != "coinbase" {
		t.Fatalf("max USD/EUR = %v from %q, want 0.91 from coinbase", res.Rate, res.Source)
	}
	if len(res.Edges) != 1 || !reflect.DeepEqual(res.Edges[0].Contributors, []string{"coinbase", "ecb"}) {
		t.Fatalf("contributors = %v", res.Edges[0].Contributors)
	}

	// Max is direction-aware: max EUR/USD is the inverse of the lowest USD/EUR.
	inv, ok := s.Resolve("EUR", "USD", now)
	if !ok {
		t.Fatal("expected EUR/USD")
	}
	if !almost(inv.Rate, 1/0.90) || inv.Source != "ecb" {
		t.Fatalf("max EUR/USD = %v from %q, want %v from ecb", inv.Rate, inv.Source, 1/0.90)
	}
}

func TestMinStrategy(t *testing.T) {
	now := time.Now().UTC()
	edges := []weightedEdge{
		we("USD", "EUR", 0.90, "ecb", model.ClassOfficial, 1, now),
		we("USD", "EUR", 0.91, "coinbase", model.ClassMarket, 3, now),
	}
	s := newTestStateWithStrategy(t, edges, now, model.StrategyMin)

	res, ok := s.Resolve("USD", "EUR", now)
	if !ok {
		t.Fatal("expected USD/EUR")
	}
	if !almost(res.Rate, 0.90) || res.Source != "ecb" {
		t.Fatalf("min USD/EUR = %v from %q, want 0.90 from ecb", res.Rate, res.Source)
	}

	inv, ok := s.Resolve("EUR", "USD", now)
	if !ok {
		t.Fatal("expected EUR/USD")
	}
	if !almost(inv.Rate, 1/0.91) || inv.Source != "coinbase" {
		t.Fatalf("min EUR/USD = %v from %q, want %v from coinbase", inv.Rate, inv.Source, 1/0.91)
	}
}

func TestAvgStrategy(t *testing.T) {
	now := time.Now().UTC()
	edges := []weightedEdge{
		we("USD", "EUR", 0.90, "ecb", model.ClassOfficial, 1, now),
		we("USD", "EUR", 0.91, "coinbase", model.ClassMarket, 3, now),
	}
	s := newTestStateWithStrategy(t, edges, now, model.StrategyAvg)

	res, ok := s.Resolve("USD", "EUR", now)
	if !ok {
		t.Fatal("expected USD/EUR")
	}
	if !almost(res.Rate, 0.905) {
		t.Fatalf("avg USD/EUR = %v, want 0.905", res.Rate)
	}
	if res.Source != "avg" || len(res.Edges) != 1 || res.Edges[0].Class != model.ClassAggregator {
		t.Fatalf("source/class = %q/%v", res.Source, res.Edges[0].Class)
	}
	if !reflect.DeepEqual(res.Edges[0].Contributors, []string{"coinbase", "ecb"}) {
		t.Fatalf("contributors = %v", res.Edges[0].Contributors)
	}
}

func TestMedianStrategy(t *testing.T) {
	now := time.Now().UTC()
	edges := []weightedEdge{
		we("USD", "EUR", 0.90, "ecb", model.ClassOfficial, 1, now),
		we("USD", "EUR", 0.92, "erapi", model.ClassAggregator, 2, now),
		we("USD", "EUR", 0.91, "coinbase", model.ClassMarket, 3, now),
	}
	s := newTestStateWithStrategy(t, edges, now, model.StrategyMedian)

	res, ok := s.Resolve("USD", "EUR", now)
	if !ok {
		t.Fatal("expected USD/EUR")
	}
	if !almost(res.Rate, 0.91) {
		t.Fatalf("median USD/EUR = %v, want 0.91", res.Rate)
	}
	if res.Source != "median" {
		t.Fatalf("source = %q, want median", res.Source)
	}
	if !reflect.DeepEqual(res.Edges[0].Contributors, []string{"coinbase", "ecb", "erapi"}) {
		t.Fatalf("contributors = %v", res.Edges[0].Contributors)
	}
}

func TestSingleSourceAvgIsIdentity(t *testing.T) {
	now := time.Now().UTC()
	s := newTestStateWithStrategy(t, []weightedEdge{
		we("USD", "EUR", 0.90, "ecb", model.ClassOfficial, 1, now),
	}, now, model.StrategyAvg)

	res, ok := s.Resolve("USD", "EUR", now)
	if !ok {
		t.Fatal("expected USD/EUR")
	}
	if res.Source != "ecb" || res.Edges[0].Class != model.ClassOfficial {
		t.Fatalf("single-source avg should stay official, got %q/%v", res.Source, res.Edges[0].Class)
	}
	if len(res.Edges[0].Contributors) != 0 {
		t.Fatalf("contributors = %v, want none", res.Edges[0].Contributors)
	}
}

func TestPerKindStrategy(t *testing.T) {
	now := time.Now().UTC()
	edges := []weightedEdge{
		we("USD", "EUR", 0.90, "ecb", model.ClassOfficial, 1, now),
		we("USD", "EUR", 0.91, "coinbase", model.ClassMarket, 3, now),
		we("BTC", "USDT", 64000, "binance", model.ClassMarket, 0, now),
		we("BTC", "USDT", 65000, "coinbase", model.ClassMarket, 1, now),
	}
	s := newState(1, now, map[string]model.Asset{}, edges, testStale, func(from, to string) model.Strategy {
		if from == "BTC" || to == "BTC" {
			return model.StrategyMax
		}
		return model.StrategyPriority
	})

	fiat, ok := s.Resolve("USD", "EUR", now)
	if !ok || !almost(fiat.Rate, 0.90) {
		t.Fatalf("fiat rate = %v, want priority result 0.90", fiat.Rate)
	}
	crypto, ok := s.Resolve("BTC", "USDT", now)
	if !ok || !almost(crypto.Rate, 65000) || crypto.Source != "coinbase" {
		t.Fatalf("crypto rate = %v from %q, want max 65000 from coinbase", crypto.Rate, crypto.Source)
	}
}
