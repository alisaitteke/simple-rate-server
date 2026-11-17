package ecb

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alisaitteke/simple-rate-server/internal/provider"
)

const sampleXML = `<?xml version="1.0" encoding="UTF-8"?>
<gesmes:Envelope xmlns:gesmes="http://www.gesmes.org/xml/2002-08-01" xmlns="http://www.ecb.int/vocabulary/2002-08-01/eurofxref">
  <gesmes:subject>Reference rates</gesmes:subject>
  <Cube>
    <Cube time='2026-11-16'>
      <Cube currency='USD' rate='1.1014'/>
      <Cube currency='TRY' rate='55.5020'/>
      <Cube currency='JPY' rate='175.78'/>
    </Cube>
  </Cube>
</gesmes:Envelope>`

func TestFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, sampleXML)
	}))
	defer srv.Close()

	p := New(time.Hour, provider.NewHTTPClient(5*time.Second, "test"))
	p.URL = srv.URL

	edges, err := p.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(edges) != 3 {
		t.Fatalf("edges = %d, want 3", len(edges))
	}
	found := false
	for _, e := range edges {
		if e.From != "EUR" {
			t.Fatalf("edge base = %q, want EUR", e.From)
		}
		if e.To == "USD" {
			found = true
			if e.Rate != 1.1014 {
				t.Fatalf("EUR/USD = %v, want 1.1014", e.Rate)
			}
			if got := e.RateDate.Format("2006-01-02"); got != "2026-11-16" {
				t.Fatalf("rate date = %s", got)
			}
			if e.Class != "official" {
				t.Fatalf("class = %s", e.Class)
			}
		}
	}
	if !found {
		t.Fatal("EUR/USD edge missing")
	}
}

func TestFetchRejectsGarbage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "not xml")
	}))
	defer srv.Close()

	p := New(time.Hour, provider.NewHTTPClient(5*time.Second, "test"))
	p.URL = srv.URL
	if _, err := p.Fetch(context.Background()); err == nil {
		t.Fatal("expected an error for garbage input")
	}
}
