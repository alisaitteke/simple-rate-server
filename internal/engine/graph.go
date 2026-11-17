package engine

import (
	"container/heap"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alisaitteke/simple-rate-server/internal/model"
)

const (
	hopCost    = 1000
	rankCost   = 100
	maxHops    = 4
	maxAgeCost = 50
)

type weightedEdge struct {
	edge model.Edge
	rank int
}

// State is an immutable snapshot of the currency graph. It is swapped
// atomically on every refresh; lookups are lock-free.
type State struct {
	Version uint64
	BuiltAt time.Time
	Assets  map[string]model.Asset

	adj   map[string]map[string]weightedEdge
	cache sync.Map // pair key -> *coreResult
	stale func(model.Class) time.Duration
}

type coreResult struct {
	rate     float64
	path     []string
	edges    []model.Edge
	asOf     time.Time
	rateDate time.Time
	source   string
}

type step struct {
	from string
	edge model.Edge
}

func newState(version uint64, builtAt time.Time, assets map[string]model.Asset, edges []weightedEdge, stale func(model.Class) time.Duration, strategyFor func(from, to string) model.Strategy) *State {
	s := &State{
		Version: version,
		BuiltAt: builtAt,
		Assets:  assets,
		adj:     make(map[string]map[string]weightedEdge),
		stale:   stale,
	}

	// Every quote is also a valid observation of the reverse pair. Group the
	// candidates per directed pair, then reduce each group to one graph edge
	// with the configured strategy.
	groups := make(map[string][]candidate, len(edges)*2)
	for _, we := range edges {
		if we.edge.Rate <= 0 || math.IsNaN(we.edge.Rate) || math.IsInf(we.edge.Rate, 0) {
			continue
		}
		add := func(c candidate) {
			key := pairKey(c.edge.From, c.edge.To)
			groups[key] = append(groups[key], c)
		}
		add(candidate{edge: we.edge, rank: we.rank})
		add(candidate{edge: we.edge.Inverse(), rank: we.rank})
	}

	for key, candidates := range groups {
		i := strings.IndexByte(key, 0)
		from, to := key[:i], key[i+1:]
		strategy := model.StrategyPriority
		if strategyFor != nil {
			strategy = strategyFor(from, to)
		}
		agg, ok := aggregate(candidates, strategy, from, to)
		if !ok {
			continue
		}
		if s.adj[from] == nil {
			s.adj[from] = make(map[string]weightedEdge)
		}
		s.adj[from][to] = agg
	}
	return s
}

type candidate struct {
	edge model.Edge
	rank int
}

func aggregate(candidates []candidate, strategy model.Strategy, from, to string) (weightedEdge, bool) {
	if len(candidates) == 0 {
		return weightedEdge{}, false
	}

	// A source can quote both directions of the same pair. Count it once,
	// preferring the fresher observation and the direct orientation.
	bySource := make(map[string]candidate, len(candidates))
	for _, c := range candidates {
		if c.edge.Source == "" {
			c.edge.Source = "unknown"
		}
		cur, ok := bySource[c.edge.Source]
		if !ok || betterCandidate(c, cur, from) {
			bySource[c.edge.Source] = c
		}
	}
	list := make([]candidate, 0, len(bySource))
	for _, c := range bySource {
		list = append(list, c)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].edge.Rate != list[j].edge.Rate {
			return list[i].edge.Rate < list[j].edge.Rate
		}
		return list[i].edge.Source < list[j].edge.Source
	})

	switch strategy {
	case model.StrategyMax:
		return pickExtreme(list, true), true
	case model.StrategyMin:
		return pickExtreme(list, false), true
	case model.StrategyAvg, model.StrategyMedian:
		if len(list) == 1 {
			return weightedEdge{edge: list[0].edge, rank: list[0].rank}, true
		}
		rate := list[0].edge.Rate
		if strategy == model.StrategyMedian {
			n := len(list)
			if n%2 == 1 {
				rate = list[n/2].edge.Rate
			} else {
				rate = (list[n/2-1].edge.Rate + list[n/2].edge.Rate) / 2
			}
		} else {
			sum := 0.0
			for _, c := range list {
				sum += c.edge.Rate
			}
			rate = sum / float64(len(list))
		}
		rank := list[0].rank
		oldestFetch := list[0].edge.FetchedAt
		oldestDate := list[0].edge.RateDate
		for _, c := range list[1:] {
			if c.rank < rank {
				rank = c.rank
			}
			if c.edge.FetchedAt.Before(oldestFetch) {
				oldestFetch = c.edge.FetchedAt
			}
			if !c.edge.RateDate.IsZero() && (oldestDate.IsZero() || c.edge.RateDate.Before(oldestDate)) {
				oldestDate = c.edge.RateDate
			}
		}
		return weightedEdge{
			edge: model.Edge{
				From:         from,
				To:           to,
				Rate:         rate,
				Source:       string(strategy),
				Class:        model.ClassAggregator,
				RateDate:     oldestDate,
				FetchedAt:    oldestFetch,
				Contributors: sourceList(list),
			},
			rank: rank,
		}, true
	default: // priority
		winner := list[0]
		for _, c := range list[1:] {
			if c.rank < winner.rank || (c.rank == winner.rank && c.edge.FetchedAt.After(winner.edge.FetchedAt)) {
				winner = c
			}
		}
		return weightedEdge{edge: winner.edge, rank: winner.rank}, true
	}
}

func pickExtreme(list []candidate, max bool) weightedEdge {
	winner := list[0]
	for _, c := range list[1:] {
		if max {
			if c.edge.Rate > winner.edge.Rate || (c.edge.Rate == winner.edge.Rate && betterTie(c, winner)) {
				winner = c
			}
		} else if c.edge.Rate < winner.edge.Rate || (c.edge.Rate == winner.edge.Rate && betterTie(c, winner)) {
			winner = c
		}
	}
	we := weightedEdge{edge: winner.edge, rank: winner.rank}
	if len(list) > 1 {
		we.edge.Contributors = sourceList(list)
	}
	return we
}

func betterCandidate(a, b candidate, from string) bool {
	if a.edge.FetchedAt.After(b.edge.FetchedAt) {
		return true
	}
	if b.edge.FetchedAt.After(a.edge.FetchedAt) {
		return false
	}
	aDirect := a.edge.From == from
	bDirect := b.edge.From == from
	if aDirect != bDirect {
		return aDirect
	}
	return a.rank < b.rank
}

func betterTie(a, b candidate) bool {
	if a.rank != b.rank {
		return a.rank < b.rank
	}
	return a.edge.FetchedAt.After(b.edge.FetchedAt)
}

func sourceList(list []candidate) []string {
	out := make([]string, 0, len(list))
	for _, c := range list {
		out = append(out, c.edge.Source)
	}
	sort.Strings(out)
	return out
}

// HasData reports whether the snapshot contains at least one quote.
func (s *State) HasData() bool { return len(s.adj) > 0 }

// Resolve returns the best path from -> to as of now.
func (s *State) Resolve(from, to string, now time.Time) (model.Resolved, bool) {
	if from == to {
		return model.Resolved{
			From: from, To: to, Rate: 1,
			Path: []string{from}, AsOf: now, RateDate: now, Source: "identity",
		}, true
	}
	key := pairKey(from, to)
	var core *coreResult
	if v, ok := s.cache.Load(key); ok {
		core = v.(*coreResult)
	} else {
		c, ok := s.dijkstra(from, to, now)
		if !ok {
			return model.Resolved{}, false
		}
		core = c
		s.cache.Store(key, core)
	}

	res := model.Resolved{
		From:     from,
		To:       to,
		Rate:     core.rate,
		Path:     core.path,
		Edges:    core.edges,
		AsOf:     core.asOf,
		RateDate: core.rateDate,
		Source:   core.source,
	}
	if s.stale != nil {
		for _, e := range core.edges {
			if now.Sub(e.FetchedAt) > s.stale(e.Class) {
				res.Stale = true
				break
			}
		}
	}
	return res, true
}

func (s *State) dijkstra(from, to string, now time.Time) (*coreResult, bool) {
	dist := map[string]int{from: 0}
	hops := map[string]int{from: 0}
	prev := map[string]step{}

	pq := &priorityQueue{}
	heap.Init(pq)
	heap.Push(pq, &pqItem{code: from})

	for pq.Len() > 0 {
		it := heap.Pop(pq).(*pqItem)
		if it.code == to {
			break
		}
		if d, ok := dist[it.code]; ok && it.cost > d {
			continue
		}
		if it.hops >= maxHops {
			continue
		}
		for next, we := range s.adj[it.code] {
			nh := it.hops + 1
			if nh > maxHops {
				continue
			}
			nc := it.cost + hopCost + rankCost*we.rank + agePenalty(we.edge, now)
			if d, ok := dist[next]; ok && nc >= d {
				continue
			}
			dist[next] = nc
			hops[next] = nh
			prev[next] = step{from: it.code, edge: we.edge}
			heap.Push(pq, &pqItem{code: next, cost: nc, hops: nh})
		}
	}

	if _, ok := dist[to]; !ok {
		return nil, false
	}

	pathRev := []string{to}
	edgesRev := make([]model.Edge, 0, maxHops)
	cur := to
	for cur != from {
		st, ok := prev[cur]
		if !ok {
			return nil, false
		}
		edgesRev = append(edgesRev, st.edge)
		pathRev = append(pathRev, st.from)
		cur = st.from
	}
	path := make([]string, len(pathRev))
	edges := make([]model.Edge, len(edgesRev))
	for i := range pathRev {
		path[len(pathRev)-1-i] = pathRev[i]
	}
	for i := range edgesRev {
		edges[len(edgesRev)-1-i] = edgesRev[i]
	}

	rate := 1.0
	asOf := edges[0].FetchedAt
	rateDate := time.Time{}
	for _, e := range edges {
		rate *= e.Rate
		if e.FetchedAt.Before(asOf) {
			asOf = e.FetchedAt
		}
		if !e.RateDate.IsZero() && (rateDate.IsZero() || e.RateDate.Before(rateDate)) {
			rateDate = e.RateDate
		}
	}
	return &coreResult{
		rate:     rate,
		path:     path,
		edges:    edges,
		asOf:     asOf,
		rateDate: rateDate,
		source:   edges[0].Source,
	}, true
}

func agePenalty(e model.Edge, now time.Time) int {
	if e.FetchedAt.IsZero() {
		return maxAgeCost
	}
	m := int(now.Sub(e.FetchedAt).Minutes())
	if m < 0 {
		return 0
	}
	if m > maxAgeCost {
		return maxAgeCost
	}
	return m
}

func pairKey(from, to string) string { return from + "\x00" + to }

type pqItem struct {
	code  string
	cost  int
	hops  int
	index int
}

type priorityQueue []*pqItem

func (pq priorityQueue) Len() int { return len(pq) }

func (pq priorityQueue) Less(i, j int) bool {
	if pq[i].cost != pq[j].cost {
		return pq[i].cost < pq[j].cost
	}
	return pq[i].hops < pq[j].hops
}

func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *priorityQueue) Push(x any) {
	item := x.(*pqItem)
	item.index = len(*pq)
	*pq = append(*pq, item)
}

func (pq *priorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	*pq = old[:n-1]
	return item
}
