# rate-server

Self-hosted exchange-rate server that pulls fiat and crypto rates from seven
keyless sources, keeps the last good snapshot in memory, and serves it as JSON.

Free rate APIs change terms or disappear. This server keeps the keyless ones
behind one local endpoint, needs no account or database, and falls back to
another source when one is down.

## Run it

```bash
go run ./cmd/rate-server
```

The listener comes up immediately. Provider fetches run in the background, so
`/readyz` returns 200 once the first source has delivered data.

```bash
curl localhost:8080/readyz
curl 'localhost:8080/v1/latest?base=USD&symbols=EUR,TRY,BTC'
```

```json
{
  "amount": 1,
  "base": "USD",
  "date": "2026-11-16",
  "rates": {"BTC": 0.00001526, "EUR": 0.9079, "TRY": 50.405},
  "stale": false
}
```

With Docker:

```bash
docker build -t rate-server .
docker run --rm -p 8080:8080 rate-server
```

Go 1.27 or later is enough for the local run. Docker is only needed for the
image.

## API

| Endpoint | Purpose |
|---|---|
| `GET /v1/latest?base=USD&symbols=EUR,TRY,BTC` | Current rates. `symbols=*` returns every enabled asset. |
| `GET /v1/latest?...&detail=1` | Same, with source, class, date, path and age per rate. |
| `GET /v1/latest?...&amount=100&precision=2` | Convert an amount and round the result. |
| `GET /v1/currencies?kind=fiat\|crypto` | Enabled asset catalogue. |
| `GET /v1/sources` | Provider health and the active aggregation policy. |
| `GET /healthz`, `GET /readyz` | Liveness and readiness. |

With `detail=1`, each rate carries `source`, `class`, `rate_date`, `path`,
`age_seconds`, `stale` and, when the aggregation strategy combined several
sources, `contributors`. Errors use
`{"error":{"code":"...","message":"..."}}`. Parameters, status codes and
response shapes: [docs/api.md](docs/api.md).

## Configuration

Environment and YAML use the same keys. Flags win over environment, then the
file, then defaults.

| Key | Default | Notes |
|---|---|---|
| `RATE_PORT` | `8080` | HTTP port. |
| `RATE_BIND` | `""` | `""` binds all interfaces. Use `127.0.0.1` for local-only. |
| `RATE_SOURCES` | all seven | Comma list. |
| `RATE_CRYPTO` | 12 assets | Comma list, or `none`. |
| `RATE_STRATEGY` | `priority` | `priority`, `max`, `min`, `avg`, `median`. |
| `RATE_STRATEGY_FIAT` | inherits | Per-kind override. |
| `RATE_STRATEGY_CRYPTO` | inherits | Per-kind override. |
| `RATE_LOG_FORMAT` | `json` | `json` or `text`. |

The full table, including poll intervals, staleness thresholds and flags, is in
[docs/configuration.md](docs/configuration.md). A ready-to-edit file is
[config.example.yaml](config.example.yaml).

## Aggregation

`priority` (the default) keeps the best source and is inverse-consistent. `max`,
`min`, `avg` and `median` are direction-aware: useful for best/worst-case and
consensus views, but `rate(A,B) × rate(B,A)` will not be exactly 1. Use
`priority` for accounting. Set `RATE_STRATEGY`, or the `_FIAT`/`_CRYPTO`
overrides; the trade-offs are in [docs/aggregation.md](docs/aggregation.md).

```bash
RATE_STRATEGY=median RATE_STRATEGY_CRYPTO=max go run ./cmd/rate-server
```

## Sources

Seven keyless sources: ECB (official, 6h), TCMB (official, 1h), Binance and
Coinbase (market, 30s), CoinGecko (5m), exchangerate-api (6h) and fawazahmed0
(12h). Coverage and attribution: [docs/sources.md](docs/sources.md).

## How it works

Quotes become edges in a currency graph; Dijkstra picks the path with the fewest
hops, then source rank, then age, capped at four hops. USDT and USDC are separate
assets, so a depeg stays visible. Each refresh builds an immutable snapshot and
swaps one pointer, so reads take no locks. Scheduling and graph details:
[docs/development.md](docs/development.md).

## Operations

- The listener starts before the first fetch; `/v1/latest` returns 503 with
  `Retry-After: 5` until `/readyz` is 200.
- A failing provider leaves its last good quote in service as `stale`.
  `/v1/sources` shows the error and retry time.
- No auth and no write endpoint. Bind to localhost or a private network, or put
  a reverse proxy with TLS in front.
- No database; state lives in memory. A restart is a cold start. Expect
  30–80 MB RSS and about 50–100 MB/day of upstream traffic at the default
  intervals.
- The Docker image is distroless and non-root. Use HTTP probes; there is no
  shell in the image.

## Development

```bash
make test
make race
make vet
make build
```

Layout, tests and adding a source: [docs/development.md](docs/development.md).

## Limits

No history endpoint, no metrics endpoint, a curated 12-asset crypto list, and
float rates that are unrounded by default. Use `precision` at the edge for
display, and keep settlement accounting on your own controls.

## License

MIT. See [LICENSE](LICENSE). Data stays subject to the upstream terms listed in
[docs/sources.md](docs/sources.md).
