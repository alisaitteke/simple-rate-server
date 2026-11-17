package coingecko

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alisaitteke/simple-rate-server/internal/provider"
)

func TestFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("vs_currencies"); got != "usd,eur,try" {
			t.Errorf("vs_currencies = %q", got)
		}
		_, _ = io.WriteString(w, `{
			"bitcoin":{"usd":64878,"eur":59092,"try":3270973},
			"ethereum":{"usd":1823.56,"eur":1660.93,"try":91939},
			"unknown-coin":{"usd":1}
		}`)
	}))
	defer srv.Close()

	p := New(5*time.Minute, provider.NewHTTPClient(5*time.Second, "test"), []string{"BTC", "ETH"})
	p.URL = srv.URL

	edges, err := p.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(edges) != 6 {
		t.Fatalf("edges = %d, want 6", len(edges))
	}
	got := map[string]float64{}
	for _, e := range edges {
		got[e.From+"/"+e.To] = e.Rate
	}
	if got["BTC/TRY"] != 3270973 || got["ETH/USD"] != 1823.56 {
		t.Fatalf("rates = %v", got)
	}
}

func TestFetchWithoutAssets(t *testing.T) {
	p := New(time.Minute, provider.NewHTTPClient(time.Second, "test"), nil)
	if _, err := p.Fetch(context.Background()); err == nil {
		t.Fatal("expected an error when no crypto assets are enabled")
	}
}
