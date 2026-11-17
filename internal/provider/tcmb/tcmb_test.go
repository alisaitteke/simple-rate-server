package tcmb

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
<Tarih_Date Tarih="17.11.2026" Date="11/17/2026" Bulten_No="2026/216">
  <Currency CrossOrder="0" Kod="USD" CurrencyCode="USD">
    <Unit>1</Unit>
    <ForexBuying>50.3292</ForexBuying>
    <ForexSelling>50.4199</ForexSelling>
  </Currency>
  <Currency CrossOrder="5" Kod="JPY" CurrencyCode="JPY">
    <Unit>100</Unit>
    <ForexBuying>33.5000</ForexBuying>
    <ForexSelling>33.7000</ForexSelling>
  </Currency>
  <Currency CrossOrder="9" Kod="XXX" CurrencyCode="XXX">
    <Unit>1</Unit>
    <ForexBuying/>
    <ForexSelling/>
  </Currency>
</Tarih_Date>`

func TestFetchNormalizesUnitAndMid(t *testing.T) {
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
	if len(edges) != 2 {
		t.Fatalf("edges = %d, want 2 (XXX has no quotes)", len(edges))
	}

	got := map[string]float64{}
	dates := map[string]string{}
	for _, e := range edges {
		if e.To != "TRY" {
			t.Fatalf("edge %s/%s: quote must be TRY", e.From, e.To)
		}
		got[e.From] = e.Rate
		dates[e.From] = e.RateDate.Format("2006-01-02")
	}

	if want := (50.3292 + 50.4199) / 2; !almost(got["USD"], want) {
		t.Fatalf("USD/TRY = %v, want mid %v", got["USD"], want)
	}
	// JPY is quoted per 100 units, so the edge must be divided by 100.
	if want := (33.5 + 33.7) / 2 / 100; !almost(got["JPY"], want) {
		t.Fatalf("JPY/TRY = %v, want %v", got["JPY"], want)
	}
	if dates["USD"] != "2026-11-17" {
		t.Fatalf("rate date = %s", dates["USD"])
	}
}

func almost(a, b float64) bool {
	d := a - b
	return d < 1e-12 && d > -1e-12
}
