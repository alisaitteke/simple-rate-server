package httpapi

import (
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alisaitteke/simple-rate-server/internal/model"
)

const maxSymbols = 500

type detailRate struct {
	Rate         float64     `json:"rate"`
	Value        float64     `json:"value"`
	Source       string      `json:"source"`
	Class        model.Class `json:"class"`
	RateDate     string      `json:"rate_date,omitempty"`
	Path         []string    `json:"path"`
	AgeSeconds   int64       `json:"age_seconds"`
	Stale        bool        `json:"stale"`
	Contributors []string    `json:"contributors,omitempty"`
}

type latestResponse struct {
	Amount  float64  `json:"amount"`
	Base    string   `json:"base"`
	AsOf    string   `json:"as_of,omitempty"`
	Date    string   `json:"date"`
	Stale   bool     `json:"stale"`
	Rates   any      `json:"rates"`
	Missing []string `json:"missing,omitempty"`
}

func (s *Server) handleLatest(w http.ResponseWriter, r *http.Request) {
	st := s.engine.State()
	if st == nil || !st.HasData() {
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusServiceUnavailable, "not_ready", "no rates loaded yet", "")
		return
	}

	q := r.URL.Query()
	baseCode := strings.TrimSpace(q.Get("base"))
	if baseCode == "" {
		baseCode = "USD"
	}
	base, ok := s.engine.ResolveAsset(baseCode)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown_asset", fmt.Sprintf("unknown or disabled asset %q", baseCode), "base")
		return
	}

	var kindFilter model.Kind
	switch strings.ToLower(strings.TrimSpace(q.Get("kind"))) {
	case "":
	case "fiat":
		kindFilter = model.KindFiat
	case "crypto":
		kindFilter = model.KindCrypto
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "kind must be fiat or crypto", "kind")
		return
	}

	symbols, errCode, errMsg, param := s.symbols(q.Get("symbols"), base, kindFilter)
	if errCode != "" {
		writeError(w, http.StatusBadRequest, errCode, errMsg, param)
		return
	}

	amount := 1.0
	if raw := strings.TrimSpace(q.Get("amount")); raw != "" {
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 || v > 1e15 {
			writeError(w, http.StatusBadRequest, "invalid_request", "amount must be a positive number", "amount")
			return
		}
		amount = v
	}

	precision := -1
	if raw := strings.TrimSpace(q.Get("precision")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 18 {
			writeError(w, http.StatusBadRequest, "invalid_request", "precision must be between 0 and 18", "precision")
			return
		}
		precision = n
	}

	detailRaw := strings.ToLower(strings.TrimSpace(q.Get("detail")))
	detail := detailRaw == "1" || detailRaw == "true" || detailRaw == "yes"

	now := time.Now().UTC()
	simple := make(map[string]float64, len(symbols))
	detailed := make(map[string]detailRate, len(symbols))
	missing := make([]string, 0)
	var oldestDate, oldestAsOf time.Time
	stale := false
	resolved := 0

	for _, code := range symbols {
		res, ok := st.Resolve(base.Code, code, now)
		if !ok {
			missing = append(missing, code)
			continue
		}
		resolved++
		value := roundValue(res.Rate*amount, precision)
		if res.Stale {
			stale = true
		}
		if !res.RateDate.IsZero() && (oldestDate.IsZero() || res.RateDate.Before(oldestDate)) {
			oldestDate = res.RateDate
		}
		if oldestAsOf.IsZero() || res.AsOf.Before(oldestAsOf) {
			oldestAsOf = res.AsOf
		}
		if detail {
			age := int64(now.Sub(res.AsOf).Seconds())
			if age < 0 {
				age = 0
			}
			class := model.ClassOfficial
			if len(res.Edges) > 0 {
				class = res.Edges[0].Class
			}
			dr := detailRate{
				Rate:       roundValue(res.Rate, precision),
				Value:      value,
				Source:     res.Source,
				Class:      class,
				Path:       res.Path,
				AgeSeconds: age,
				Stale:      res.Stale,
			}
			if contributors := collectContributors(res.Edges); len(contributors) > 1 {
				dr.Contributors = contributors
			}
			if !res.RateDate.IsZero() {
				dr.RateDate = res.RateDate.Format("2006-01-02")
			}
			detailed[code] = dr
		} else {
			simple[code] = value
		}
	}

	if resolved == 0 {
		writeError(w, http.StatusNotFound, "no_rate_available",
			fmt.Sprintf("no path from %s to the requested symbols", base.Code), "symbols")
		return
	}

	date := oldestDate
	if date.IsZero() {
		date = now.Truncate(24 * time.Hour)
	}
	resp := latestResponse{
		Amount:  amount,
		Base:    base.Code,
		Date:    date.Format("2006-01-02"),
		Stale:   stale,
		Missing: missing,
	}
	if !oldestAsOf.IsZero() {
		resp.AsOf = oldestAsOf.Format(time.RFC3339)
	}
	if detail {
		resp.Rates = detailed
	} else {
		resp.Rates = simple
	}

	etag := makeETag(st.Version, r.URL.RawQuery)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=5")
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) symbols(raw string, base model.Asset, kind model.Kind) (symbols []string, errCode, errMsg, param string) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "*" {
		for _, a := range s.engine.Assets(kind) {
			if a.Code != base.Code {
				symbols = append(symbols, a.Code)
			}
		}
		if len(symbols) > maxSymbols {
			symbols = symbols[:maxSymbols]
		}
		return symbols, "", "", ""
	}

	seen := make(map[string]bool)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		a, ok := s.engine.ResolveAsset(part)
		if !ok {
			return nil, "unknown_asset", fmt.Sprintf("unknown or disabled asset %q", part), "symbols"
		}
		if kind != "" && a.Kind != kind {
			continue
		}
		if a.Code == base.Code || seen[a.Code] {
			continue
		}
		seen[a.Code] = true
		symbols = append(symbols, a.Code)
		if len(symbols) >= maxSymbols {
			break
		}
	}
	if len(symbols) == 0 {
		return nil, "invalid_request", "no symbols to convert", "symbols"
	}
	return symbols, "", "", ""
}

func collectContributors(edges []model.Edge) []string {
	set := make(map[string]bool)
	for _, e := range edges {
		for _, source := range e.Contributors {
			if source != "" {
				set[source] = true
			}
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for source := range set {
		out = append(out, source)
	}
	sort.Strings(out)
	return out
}

func makeETag(version uint64, rawQuery string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(rawQuery))
	return fmt.Sprintf("\"v%d-%08x\"", version, h.Sum32())
}

func roundValue(v float64, precision int) float64 {
	if precision < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	p := math.Pow(10, float64(precision))
	return math.Round(v*p) / p
}
