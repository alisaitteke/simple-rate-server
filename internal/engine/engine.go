// Package engine schedules provider refreshes, builds the currency graph and
// resolves pairs with source-priority and shortest-path routing.
package engine

import (
	"context"
	"log/slog"
	"math"
	"math/rand/v2"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alisaitteke/simple-rate-server/internal/assets"
	"github.com/alisaitteke/simple-rate-server/internal/model"
	"github.com/alisaitteke/simple-rate-server/internal/provider"
)

// Options configures the engine.
type Options struct {
	Registry        *assets.Registry
	FiatPolicy      []string
	CryptoPolicy    []string
	EnabledFiat     map[string]bool
	EnabledCrypto   map[string]bool
	StaleMarket     time.Duration
	StaleOfficial   time.Duration
	StaleAggregator time.Duration
	StrategyFiat    model.Strategy
	StrategyCrypto  model.Strategy
	Logger          *slog.Logger
}

// SourceStatus is the public health view of one provider.
type SourceStatus struct {
	Name                string      `json:"name"`
	Class               model.Class `json:"class"`
	Interval            string      `json:"interval"`
	LastAttempt         string      `json:"last_attempt,omitempty"`
	LastSuccess         string      `json:"last_success,omitempty"`
	LastError           string      `json:"last_error,omitempty"`
	ConsecutiveFailures int         `json:"consecutive_failures"`
	RetryAfter          string      `json:"retry_after,omitempty"`
	NextRun             string      `json:"next_run,omitempty"`
	Edges               int         `json:"edges"`
}

type sourceState struct {
	attempt    time.Time
	success    time.Time
	failures   int
	lastError  string
	retryAfter time.Duration
	nextRun    time.Time
	edges      int
}

// Engine owns the providers and the current graph snapshot.
type Engine struct {
	opts      Options
	providers []provider.Provider
	log       *slog.Logger

	mu     sync.RWMutex
	edges  map[string][]model.Edge
	status map[string]*sourceState

	state   atomic.Pointer[State]
	version atomic.Uint64
}

// New creates an engine. Providers are fetched immediately by Start.
func New(opts Options, providers []provider.Provider) *Engine {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.EnabledFiat == nil {
		opts.EnabledFiat = map[string]bool{}
	}
	if opts.EnabledCrypto == nil {
		opts.EnabledCrypto = map[string]bool{}
	}
	if len(opts.FiatPolicy) == 0 {
		opts.FiatPolicy = []string{"tcmb", "ecb", "erapi", "coinbase", "fawazahmed"}
	}
	if len(opts.CryptoPolicy) == 0 {
		opts.CryptoPolicy = []string{"binance", "coinbase", "coingecko", "fawazahmed"}
	}
	if opts.StrategyFiat == "" {
		opts.StrategyFiat = model.StrategyPriority
	}
	if opts.StrategyCrypto == "" {
		opts.StrategyCrypto = model.StrategyPriority
	}
	e := &Engine{
		opts:      opts,
		providers: providers,
		log:       opts.Logger,
		edges:     make(map[string][]model.Edge, len(providers)),
		status:    make(map[string]*sourceState, len(providers)),
	}
	for _, p := range providers {
		e.status[p.Name()] = &sourceState{}
	}
	return e
}

// Start performs one parallel fetch, then runs each provider on its own
// schedule until ctx is cancelled.
func (e *Engine) Start(ctx context.Context, timeout time.Duration) {
	sctx, cancel := context.WithTimeout(ctx, timeout)
	e.RefreshNow(sctx)
	cancel()
	for _, p := range e.providers {
		go e.loop(ctx, p)
	}
}

// RefreshNow fetches every provider once and waits for the results.
func (e *Engine) RefreshNow(ctx context.Context) {
	var wg sync.WaitGroup
	for _, p := range e.providers {
		wg.Add(1)
		go func(p provider.Provider) {
			defer wg.Done()
			e.refresh(ctx, p)
		}(p)
	}
	wg.Wait()
}

func (e *Engine) refresh(ctx context.Context, p provider.Provider) {
	start := time.Now()
	edges, err := p.Fetch(ctx)
	now := time.Now().UTC()

	e.mu.Lock()
	st := e.status[p.Name()]
	if st == nil {
		st = &sourceState{}
		e.status[p.Name()] = st
	}
	st.attempt = now
	if err != nil {
		if ctx.Err() != nil {
			e.mu.Unlock()
			return
		}
		st.failures++
		st.lastError = err.Error()
		if d := provider.RetryDelay(err); d > st.retryAfter {
			st.retryAfter = d
		}
		failures := st.failures
		e.mu.Unlock()
		e.log.Warn("provider refresh failed",
			"source", p.Name(), "error", err, "consecutive_failures", failures)
		return
	}
	filtered := e.filterEdges(edges)
	st.success = now
	st.failures = 0
	st.lastError = ""
	st.retryAfter = 0
	st.edges = len(filtered)
	e.edges[p.Name()] = filtered
	e.mu.Unlock()

	e.rebuild(now)
	e.log.Debug("provider refreshed",
		"source", p.Name(), "edges", len(filtered), "duration", time.Since(start))
}

func (e *Engine) filterEdges(in []model.Edge) []model.Edge {
	out := make([]model.Edge, 0, len(in))
	seen := make(map[string]int, len(in))
	for _, ed := range in {
		from, ok := e.opts.Registry.Normalize(ed.From)
		if !ok || !e.enabled(from) {
			continue
		}
		to, ok := e.opts.Registry.Normalize(ed.To)
		if !ok || !e.enabled(to) {
			continue
		}
		if from == to || ed.Rate <= 0 || math.IsNaN(ed.Rate) || math.IsInf(ed.Rate, 0) {
			continue
		}
		ed.From, ed.To = from, to
		key := pairKey(from, to)
		if idx, ok := seen[key]; ok {
			if ed.FetchedAt.After(out[idx].FetchedAt) {
				out[idx] = ed
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, ed)
	}
	return out
}

func (e *Engine) enabled(code string) bool {
	a, ok := e.opts.Registry.Get(code)
	if !ok {
		return false
	}
	if a.Kind == model.KindFiat {
		return e.opts.EnabledFiat[code]
	}
	return e.opts.EnabledCrypto[code]
}

func (e *Engine) rebuild(now time.Time) {
	e.mu.RLock()
	all := make([]model.Edge, 0, 2048)
	for _, edges := range e.edges {
		all = append(all, edges...)
	}
	e.mu.RUnlock()

	weighted := make([]weightedEdge, 0, len(all))
	for _, ed := range all {
		kind := e.kindFor(ed.From, ed.To)
		weighted = append(weighted, weightedEdge{edge: ed, rank: e.rankFor(ed.Source, kind)})
	}

	assetsMap := make(map[string]model.Asset, len(e.opts.EnabledFiat)+len(e.opts.EnabledCrypto))
	for code := range e.opts.EnabledFiat {
		if a, ok := e.opts.Registry.Get(code); ok {
			assetsMap[code] = a
		}
	}
	for code := range e.opts.EnabledCrypto {
		if a, ok := e.opts.Registry.Get(code); ok {
			assetsMap[code] = a
		}
	}

	v := e.version.Add(1)
	e.state.Store(newState(v, now, assetsMap, weighted, e.staleAfter, e.strategyFor))
}

// strategyFor selects the aggregation strategy for a pair's asset kind.
func (e *Engine) strategyFor(from, to string) model.Strategy {
	if e.kindFor(from, to) == model.KindCrypto {
		return e.opts.StrategyCrypto
	}
	return e.opts.StrategyFiat
}

// Strategies returns the configured fiat and crypto aggregation strategies.
func (e *Engine) Strategies() (fiat, crypto model.Strategy) {
	return e.opts.StrategyFiat, e.opts.StrategyCrypto
}

func (e *Engine) kindFor(from, to string) model.Kind {
	a, okA := e.opts.Registry.Get(from)
	b, okB := e.opts.Registry.Get(to)
	if okA && okB && a.Kind == model.KindFiat && b.Kind == model.KindFiat {
		return model.KindFiat
	}
	return model.KindCrypto
}

func (e *Engine) rankFor(source string, kind model.Kind) int {
	policy := e.opts.FiatPolicy
	if kind == model.KindCrypto {
		policy = e.opts.CryptoPolicy
	}
	for i, s := range policy {
		if s == source {
			return i
		}
	}
	return len(policy) + 1
}

func (e *Engine) staleAfter(class model.Class) time.Duration {
	switch class {
	case model.ClassMarket:
		return e.opts.StaleMarket
	case model.ClassOfficial:
		return e.opts.StaleOfficial
	default:
		return e.opts.StaleAggregator
	}
}

// State returns the current immutable snapshot, or nil before the first fetch.
func (e *Engine) State() *State { return e.state.Load() }

// Version returns the current snapshot version.
func (e *Engine) Version() uint64 { return e.version.Load() }

// Assets returns enabled assets of the given kind. An empty kind returns all.
func (e *Engine) Assets(kind model.Kind) []model.Asset {
	all := e.opts.Registry.List(kind)
	out := make([]model.Asset, 0, len(all))
	for _, a := range all {
		if e.enabled(a.Code) {
			out = append(out, a)
		}
	}
	return out
}

// ResolveAsset normalizes code and reports whether it is enabled.
func (e *Engine) ResolveAsset(code string) (model.Asset, bool) {
	code, ok := e.opts.Registry.Normalize(code)
	if !ok || !e.enabled(code) {
		return model.Asset{}, false
	}
	return e.opts.Registry.Get(code)
}

// Status returns a snapshot of provider health ordered by name.
func (e *Engine) Status() []SourceStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]SourceStatus, 0, len(e.providers))
	for _, p := range e.providers {
		st := e.status[p.Name()]
		if st == nil {
			st = &sourceState{}
		}
		out = append(out, SourceStatus{
			Name:                p.Name(),
			Class:               p.Class(),
			Interval:            p.Interval().String(),
			LastAttempt:         formatTime(st.attempt),
			LastSuccess:         formatTime(st.success),
			LastError:           st.lastError,
			ConsecutiveFailures: st.failures,
			RetryAfter:          formatDuration(st.retryAfter),
			NextRun:             formatTime(st.nextRun),
			Edges:               st.edges,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (e *Engine) loop(ctx context.Context, p provider.Provider) {
	for {
		delay := e.nextDelay(p)
		e.setNextRun(p.Name(), time.Now().Add(delay))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if ctx.Err() != nil {
			return
		}
		e.refresh(ctx, p)
	}
}

func (e *Engine) nextDelay(p provider.Provider) time.Duration {
	e.mu.RLock()
	st := e.status[p.Name()]
	var failures int
	var lastSuccess time.Time
	var retryAfter time.Duration
	if st != nil {
		failures = st.failures
		lastSuccess = st.success
		retryAfter = st.retryAfter
	}
	e.mu.RUnlock()

	if lastSuccess.IsZero() {
		return 5 * time.Second
	}
	base := p.Interval()
	if failures > 0 {
		shift := failures
		if shift > 4 {
			shift = 4
		}
		base *= time.Duration(1 << shift)
		if base > 30*time.Minute {
			base = 30 * time.Minute
		}
	}
	if retryAfter > base {
		base = retryAfter
	}
	jitter := time.Duration(rand.Int64N(int64(base)/10+1)) - base/20
	if base+jitter > 0 {
		base += jitter
	}
	return base
}

func (e *Engine) setNextRun(name string, t time.Time) {
	e.mu.Lock()
	if st := e.status[name]; st != nil {
		st.nextRun = t
	}
	e.mu.Unlock()
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return d.String()
}
