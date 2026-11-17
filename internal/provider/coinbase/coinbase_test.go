package coinbase

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
		_, _ = io.WriteString(w, `{"data":{"currency":"USD","rates":{
			"USD":"1",
			"EUR":"0.91075",
			"TRY":"50.4035",
			"BTC":"0.0000154163",
			"USDT":"0.998985",
			"BROKEN":"not-a-number"
		}}}`)
	}))
	defer srv.Close()

	p := New(time.Minute, provider.NewHTTPClient(5*time.Second, "test"))
	p.URL = srv.URL

	edges, err := p.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(edges) != 4 {
		t.Fatalf("edges = %d, want 4", len(edges))
	}
	got := map[string]float64{}
	for _, e := range edges {
		if e.From != "USD" {
			t.Fatalf("base = %s", e.From)
		}
		got[e.To] = e.Rate
	}
	if got["TRY"] != 50.4035 || got["BTC"] != 0.0000154163 {
		t.Fatalf("rates = %v", got)
	}
	if _, ok := got["BROKEN"]; ok {
		t.Fatal("non-numeric rate should be skipped")
	}
}
