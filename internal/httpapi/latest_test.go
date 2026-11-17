package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alisaitteke/simple-rate-server/internal/assets"
	"github.com/alisaitteke/simple-rate-server/internal/config"
	"github.com/alisaitteke/simple-rate-server/internal/engine"
	"github.com/alisaitteke/simple-rate-server/internal/model"
	"github.com/alisaitteke/simple-rate-server/internal/provider"
)

type fakeProvider struct {
	edges []model.Edge
}

func (f *fakeProvider) Name() string            { return "fake" }
func (f *fakeProvider) Class() model.Class      { return model.ClassOfficial }
func (f *fakeProvider) Interval() time.Duration { return time.Hour }
func (f *fakeProvider) Fetch(context.Context) ([]model.Edge, error) {
	return f.edges, nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	reg, err := assets.New()
	if err != nil {
		t.Fatalf("assets: %v", err)
	}
	now := time.Now().UTC()
	fake := &fakeProvider{edges: []model.Edge{
		{From: "USD", To: "EUR", Rate: 0.9, Source: "fake", Class: model.ClassOfficial, RateDate: now, FetchedAt: now},
		{From: "USD", To: "TRY", Rate: 50, Source: "fake", Class: model.ClassOfficial, RateDate: now, FetchedAt: now},
		{From: "BTC", To: "USD", Rate: 65000, Source: "fake", Class: model.ClassMarket, RateDate: now, FetchedAt: now},
	}}
	eng := engine.New(engine.Options{
		Registry:        reg,
		EnabledFiat:     map[string]bool{"USD": true, "EUR": true, "TRY": true, "GBP": true},
		EnabledCrypto:   map[string]bool{"BTC": true},
		StaleMarket:     5 * time.Minute,
		StaleOfficial:   96 * time.Hour,
		StaleAggregator: 48 * time.Hour,
		Logger:          discardLogger(),
	}, []provider.Provider{fake})
	eng.RefreshNow(context.Background())

	cfg := config.Default()
	cfg.CORS = false
	srv := New(eng, cfg, discardLogger())
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func getJSON(t *testing.T, url string, out any) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s: %v", url, err)
		}
	}
	return resp
}

func TestLatestSimple(t *testing.T) {
	ts := newTestServer(t)
	var body struct {
		Amount float64            `json:"amount"`
		Base   string             `json:"base"`
		Date   string             `json:"date"`
		Stale  bool               `json:"stale"`
		Rates  map[string]float64 `json:"rates"`
	}
	resp := getJSON(t, ts.URL+"/v1/latest?base=USD&symbols=EUR,TRY", &body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if body.Base != "USD" || body.Amount != 1 {
		t.Fatalf("base/amount = %s/%v", body.Base, body.Amount)
	}
	if body.Rates["EUR"] != 0.9 || body.Rates["TRY"] != 50 {
		t.Fatalf("rates = %v", body.Rates)
	}
}

func TestLatestAppliesAmount(t *testing.T) {
	ts := newTestServer(t)
	var body struct {
		Rates map[string]float64 `json:"rates"`
	}
	getJSON(t, ts.URL+"/v1/latest?base=USD&symbols=EUR&amount=10", &body)
	if body.Rates["EUR"] != 9 {
		t.Fatalf("EUR = %v, want 9", body.Rates["EUR"])
	}
}

func TestLatestDetailExposesProvenance(t *testing.T) {
	ts := newTestServer(t)
	var body struct {
		AsOf  string `json:"as_of"`
		Rates map[string]struct {
			Rate   float64  `json:"rate"`
			Value  float64  `json:"value"`
			Source string   `json:"source"`
			Class  string   `json:"class"`
			Path   []string `json:"path"`
		} `json:"rates"`
	}
	resp := getJSON(t, ts.URL+"/v1/latest?base=USD&symbols=EUR&detail=1", &body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	eur := body.Rates["EUR"]
	if eur.Source != "fake" || eur.Class != "official" {
		t.Fatalf("provenance = %+v", eur)
	}
	if len(eur.Path) != 2 || eur.Path[0] != "USD" || eur.Path[1] != "EUR" {
		t.Fatalf("path = %v", eur.Path)
	}
	if body.AsOf == "" {
		t.Fatal("as_of must be set")
	}
}
