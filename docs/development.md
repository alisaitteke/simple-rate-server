# Development

Go 1.27 or later. Docker is only needed for the image.

## Commands

```bash
go run ./cmd/rate-server     # local run
make test                    # go test ./...
make race                    # go test -race ./...
make vet                     # go vet ./...
make build                   # bin/rate-server
make docker                  # rate-server:dev image
```

`make build` strips the binary and injects the version from `VERSION`, which
defaults to `dev`.

## Layout

```
cmd/rate-server/          flags, wiring, graceful shutdown
internal/assets/          embedded ISO 4217 and crypto catalogue
internal/config/          defaults, YAML, RATE_* env, validation
internal/model/           Asset, Edge, Strategy, Resolved
internal/provider/        Provider interface and HTTP helper
internal/provider/*/      one package per upstream
internal/engine/          graph, Dijkstra, aggregation, scheduler, atomic state
internal/httpapi/         handlers, errors, middleware
```

## Refreshes and state

Every provider runs on its own goroutine with its own interval. Start performs
one parallel fetch and then starts the loops. A provider that has never
succeeded retries after five seconds; after a failure the interval doubles, up
to 30 minutes or the provider's `Retry-After`, whichever is longer. The last
good edges stay in place, so a failed refresh makes quotes stale, not missing.

After each refresh the engine rebuilds an immutable `State` from all provider
edges and swaps a single `atomic.Pointer`. HTTP reads take no locks. Resolved
pairs are memoized inside the snapshot, so a cache entry never survives a
refresh with changed data.

## Graph and resolution

Each quote is a directed edge; the reverse edge is derived. For every directed
pair, the engine collects the direct quotes and the inverses of quotes in the
other direction, then applies the configured strategy. The result is one edge
per directed pair.

Dijkstra then minimises:

```
1000 per hop + 100 × source rank + 0..50 for age
```

Fewer hops always wins. Source rank breaks ties, and age only breaks ties
within the same source. The path is capped at four hops. Source priorities:

- fiat: `tcmb > ecb > erapi > coinbase > fawazahmed`
- crypto: `binance > coinbase > coingecko > fawazahmed`

`USDT` and `USDC` are separate assets. The bridge to USD comes from a real
`USDT/USD` or `USDC/USD` quote, so a depeg is not hidden by an alias.

## Adding a provider

Implement the interface in `internal/provider/provider.go`:

```go
type Provider interface {
    Name() string
    Class() model.Class
    Interval() time.Duration
    Fetch(ctx context.Context) ([]model.Edge, error)
}
```

Then:

1. Create `internal/provider/<name>/<name>.go`.
2. Normalize every quote to `1 From = Rate To`. Use `RateDate` for the bulletin
   date and `FetchedAt` for the fetch time. Handle unit or nominal scaling
   before creating the edge.
3. Register the constructor in `buildProviders` in `cmd/rate-server/main.go`
   and the name in `config.knownSources`.
4. Add the source to the relevant priority list in `engine.New` if it should
   compete for pairs.
5. Add a test with a trimmed fixture and an `httptest` server. No network in
   tests.

## Tests

Parser tests cover ECB and TCMB XML and the JSON sources. The engine tests check
inverse consistency, direct-over-two-hop, official-over-market at equal hops,
the stablecoin bridge, the four-hop cap, staleness, and each aggregation
strategy. HTTP tests use a fake provider and cover 400/404/503, amount and
precision handling, ETag/304, currencies, sources and readiness. Config tests
cover file/env precedence, strategy inheritance and strict rejection of unknown
keys.

## Deployment

The Docker image is distroless, runs as a non-root user and has no shell. Use
`/healthz` for liveness and `/readyz` for readiness; there is no `HEALTHCHECK`
in the image. The server is stateless, so do not mount a volume.

There is no auth. Bind to localhost or a private network, or put a reverse proxy
with TLS in front of it. The process handles `SIGINT` and `SIGTERM`, cancels
in-flight fetches and drains HTTP requests with a five-second deadline.
