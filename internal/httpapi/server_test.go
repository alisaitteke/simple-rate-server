package httpapi

import (
	"context"
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

func TestLatestUnknownAsset(t *testing.T) {
	ts := newTestServer(t)
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	resp := getJSON(t, ts.URL+"/v1/latest?base=USD&symbols=ZZZ", &body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if body.Error.Code != "unknown_asset" {
		t.Fatalf("code = %s", body.Error.Code)
	}
}

func TestLatestNoPath(t *testing.T) {
	ts := newTestServer(t)
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	resp := getJSON(t, ts.URL+"/v1/latest?base=USD&symbols=GBP", &body)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if body.Error.Code != "no_rate_available" {
		t.Fatalf("code = %s", body.Error.Code)
	}
}

func TestCurrenciesFilter(t *testing.T) {
	ts := newTestServer(t)
	var body struct {
		Count  int `json:"count"`
		Assets []struct {
			Code string `json:"code"`
			Kind string `json:"kind"`
		} `json:"assets"`
	}
	getJSON(t, ts.URL+"/v1/currencies?kind=crypto", &body)
	if body.Count != 1 || len(body.Assets) != 1 || body.Assets[0].Code != "BTC" {
		t.Fatalf("assets = %+v", body.Assets)
	}
}

func TestSourcesAndReady(t *testing.T) {
	ts := newTestServer(t)
	var sources struct {
		Ready    bool              `json:"ready"`
		Strategy map[string]string `json:"strategy"`
		Sources  []struct {
			Name string `json:"name"`
		} `json:"sources"`
	}
	getJSON(t, ts.URL+"/v1/sources", &sources)
	if !sources.Ready || len(sources.Sources) != 1 || sources.Sources[0].Name != "fake" {
		t.Fatalf("sources = %+v", sources)
	}
	if sources.Strategy["fiat"] != "priority" || sources.Strategy["crypto"] != "priority" {
		t.Fatalf("strategy = %v", sources.Strategy)
	}
	resp := getJSON(t, ts.URL+"/readyz", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("readyz = %d", resp.StatusCode)
	}
}

type namedProvider struct {
	name  string
	class model.Class
	edges []model.Edge
}

func (p *namedProvider) Name() string            { return p.name }
func (p *namedProvider) Class() model.Class      { return p.class }
func (p *namedProvider) Interval() time.Duration { return time.Hour }
func (p *namedProvider) Fetch(context.Context) ([]model.Edge, error) {
	return p.edges, nil
}

func TestLatestDetailContributorsWithMaxStrategy(t *testing.T) {
	reg, err := assets.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	alpha := &namedProvider{name: "alpha", class: model.ClassOfficial, edges: []model.Edge{
		{From: "USD", To: "EUR", Rate: 0.90, Source: "alpha", Class: model.ClassOfficial, RateDate: now, FetchedAt: now},
	}}
	beta := &namedProvider{name: "beta", class: model.ClassMarket, edges: []model.Edge{
		{From: "USD", To: "EUR", Rate: 0.92, Source: "beta", Class: model.ClassMarket, RateDate: now, FetchedAt: now},
	}}
	eng := engine.New(engine.Options{
		Registry:        reg,
		EnabledFiat:     map[string]bool{"USD": true, "EUR": true},
		StaleMarket:     5 * time.Minute,
		StaleOfficial:   96 * time.Hour,
		StaleAggregator: 48 * time.Hour,
		StrategyFiat:    model.StrategyMax,
		Logger:          discardLogger(),
	}, []provider.Provider{alpha, beta})
	eng.RefreshNow(context.Background())

	srv := New(eng, config.Default(), discardLogger())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	var body struct {
		Rates map[string]struct {
			Rate         float64  `json:"rate"`
			Source       string   `json:"source"`
			Class        string   `json:"class"`
			Contributors []string `json:"contributors"`
		} `json:"rates"`
	}
	resp := getJSON(t, ts.URL+"/v1/latest?base=USD&symbols=EUR&detail=1", &body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	eur := body.Rates["EUR"]
	if eur.Rate != 0.92 || eur.Source != "beta" || eur.Class != "market" {
		t.Fatalf("rate/source/class = %v/%q/%q", eur.Rate, eur.Source, eur.Class)
	}
	if len(eur.Contributors) != 2 || eur.Contributors[0] != "alpha" || eur.Contributors[1] != "beta" {
		t.Fatalf("contributors = %v", eur.Contributors)
	}
}

func TestETagReturns304(t *testing.T) {
	ts := newTestServer(t)
	resp := getJSON(t, ts.URL+"/v1/latest?base=USD&symbols=EUR", nil)
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("ETag header missing")
	}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/latest?base=USD&symbols=EUR", nil)
	req.Header.Set("If-None-Match", etag)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", resp2.StatusCode)
	}
}

func TestNotReady(t *testing.T) {
	reg, err := assets.New()
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.New(engine.Options{
		Registry:    reg,
		EnabledFiat: map[string]bool{"USD": true},
		Logger:      discardLogger(),
	}, nil)
	cfg := config.Default()
	srv := New(eng, cfg, discardLogger())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	if resp := getJSON(t, ts.URL+"/v1/latest?base=USD&symbols=EUR", nil); resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("latest status = %d, want 503", resp.StatusCode)
	}
	if resp := getJSON(t, ts.URL+"/readyz", nil); resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want 503", resp.StatusCode)
	}
}
