// Package assets holds the embedded catalogue of supported fiat and crypto
// assets, including display precision.
package assets

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/alisaitteke/simple-rate-server/internal/model"
)

//go:embed assets.json
var rawAssets []byte

// aliases maps provider-specific or legacy symbols to canonical codes.
var aliases = map[string]string{
	"XBT": "BTC",
	"UST": "USDT",
}

// Registry is an immutable asset catalogue.
type Registry struct {
	byCode map[string]model.Asset
}

// New parses and validates the embedded catalogue.
func New() (*Registry, error) {
	var list []model.Asset
	if err := json.Unmarshal(rawAssets, &list); err != nil {
		return nil, fmt.Errorf("parse embedded assets: %w", err)
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("embedded asset catalogue is empty")
	}
	r := &Registry{byCode: make(map[string]model.Asset, len(list))}
	for _, a := range list {
		a.Code = strings.ToUpper(strings.TrimSpace(a.Code))
		a.Name = strings.TrimSpace(a.Name)
		if a.Code == "" || !a.Kind.Valid() {
			return nil, fmt.Errorf("invalid asset %q", a.Code)
		}
		if a.Decimals < 0 || a.Decimals > 18 {
			return nil, fmt.Errorf("asset %s: invalid decimals %d", a.Code, a.Decimals)
		}
		if _, dup := r.byCode[a.Code]; dup {
			return nil, fmt.Errorf("duplicate asset %s", a.Code)
		}
		r.byCode[a.Code] = a
	}
	return r, nil
}

// Normalize upper-cases code, applies known aliases and reports whether the
// resulting code exists in the catalogue.
func (r *Registry) Normalize(code string) (string, bool) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if alias, ok := aliases[code]; ok {
		code = alias
	}
	_, ok := r.byCode[code]
	return code, ok
}

// Get returns the asset for a canonical code.
func (r *Registry) Get(code string) (model.Asset, bool) {
	a, ok := r.byCode[strings.ToUpper(strings.TrimSpace(code))]
	return a, ok
}

// Has reports whether code is a known canonical asset.
func (r *Registry) Has(code string) bool {
	_, ok := r.Get(code)
	return ok
}

// List returns assets of the given kind sorted by code. An empty kind returns
// every asset.
func (r *Registry) List(kind model.Kind) []model.Asset {
	out := make([]model.Asset, 0, len(r.byCode))
	for _, a := range r.byCode {
		if kind == "" || a.Kind == kind {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}
