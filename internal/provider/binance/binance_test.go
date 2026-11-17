package binance

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alisaitteke/simple-rate-server/internal/provider"
)

const exchangeInfoBody = `{"symbols":[
  {"symbol":"BTCUSDT","status":"TRADING","baseAsset":"BTC","quoteAsset":"USDT"},
  {"symbol":"BTCTRY","status":"TRADING","baseAsset":"BTC","quoteAsset":"TRY"},
  {"symbol":"ETHUSDT","status":"BREAK","baseAsset":"ETH","quoteAsset":"USDT"},
  {"symbol":"DOGEUSDT","status":"TRADING","baseAsset":"DOGE","quoteAsset":"USDT"}
]}`

func TestFetchUsesTargetedSymbols(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/exchangeInfo", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, exchangeInfoBody)
	})
	mux.HandleFunc("/ticker/price", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("symbols") == "" {
			t.Error("expected the targeted symbols parameter")
		}
		_, _ = io.WriteString(w, `[
			{"symbol":"BTCUSDT","price":"65000.00"},
			{"symbol":"BTCTRY","price":"3270000.00"}
		]`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := New(time.Minute, provider.NewHTTPClient(5*time.Second, "test"),
		[]string{"BTC", "ETH"}, []string{"USDT", "TRY", "BTC", "ETH"})
	p.ExchangeInfoURL = srv.URL + "/exchangeInfo"
	p.TickerURL = srv.URL + "/ticker/price"

	edges, err := p.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(edges) != 2 {
		t.Fatalf("edges = %d, want 2", len(edges))
	}
	got := map[string]float64{}
	for _, e := range edges {
		got[e.From+"/"+e.To] = e.Rate
	}
	if got["BTC/USDT"] != 65000 {
		t.Fatalf("BTC/USDT = %v", got["BTC/USDT"])
	}
	if got["BTC/TRY"] != 3270000 {
		t.Fatalf("BTC/TRY = %v", got["BTC/TRY"])
	}
}

func TestFetchFallsBackToAllTickers(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/exchangeInfo", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	mux.HandleFunc("/ticker/price", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[
			{"symbol":"BTCUSDT","price":"65000.00"},
			{"symbol":"DOGEUSDT","price":"0.12"},
			{"symbol":"USDCUSDT","price":"1.0002"}
		]`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := New(time.Minute, provider.NewHTTPClient(5*time.Second, "test"),
		[]string{"BTC"}, []string{"USDT"})
	p.ExchangeInfoURL = srv.URL + "/exchangeInfo"
	p.TickerURL = srv.URL + "/ticker/price"

	edges, err := p.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("edges = %d, want 1 (only BTC is enabled)", len(edges))
	}
	if edges[0].From != "BTC" || edges[0].To != "USDT" || edges[0].Rate != 65000 {
		t.Fatalf("edge = %+v", edges[0])
	}
}
