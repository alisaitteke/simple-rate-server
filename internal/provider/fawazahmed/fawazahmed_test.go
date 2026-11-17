package fawazahmed

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alisaitteke/simple-rate-server/internal/provider"
)

func TestFetchFallsBackToSecondMirror(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusInternalServerError)
	}))
	defer bad.Close()

	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{
			"date":"2026-11-16",
			"usd":{"eur":0.90794,"btc":0.0000154163,"zzz":1}
		}`)
	}))
	defer good.Close()

	p := New(12*time.Hour, provider.NewHTTPClient(5*time.Second, "test"), []string{"EUR", "BTC"})
	p.URLs = []string{bad.URL, good.URL}

	edges, err := p.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(edges) != 2 {
		t.Fatalf("edges = %d, want 2", len(edges))
	}
	for _, e := range edges {
		if e.From != "USD" {
			t.Fatalf("base = %s", e.From)
		}
		if got := e.RateDate.Format("2006-01-02"); got != "2026-11-16" {
			t.Fatalf("rate date = %s", got)
		}
	}
}
