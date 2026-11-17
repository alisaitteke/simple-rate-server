package erapi

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
		_, _ = io.WriteString(w, `{
			"result":"success",
			"base_code":"USD",
			"time_last_update_unix":1763337600,
			"rates":{"USD":1,"EUR":0.90794,"TRY":50.392}
		}`)
	}))
	defer srv.Close()

	p := New(time.Hour, provider.NewHTTPClient(5*time.Second, "test"))
	p.URL = srv.URL

	edges, err := p.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(edges) != 2 {
		t.Fatalf("edges = %d, want 2 (base itself is skipped)", len(edges))
	}
	rates := map[string]float64{}
	for _, e := range edges {
		if e.From != "USD" {
			t.Fatalf("base = %s, want USD", e.From)
		}
		rates[e.To] = e.Rate
	}
	if rates["EUR"] != 0.90794 || rates["TRY"] != 50.392 {
		t.Fatalf("rates = %v", rates)
	}
}

func TestFetchRejectsFailureResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"result":"error","rates":{}}`)
	}))
	defer srv.Close()

	p := New(time.Hour, provider.NewHTTPClient(5*time.Second, "test"))
	p.URL = srv.URL
	if _, err := p.Fetch(context.Background()); err == nil {
		t.Fatal("expected an error for result=error")
	}
}
