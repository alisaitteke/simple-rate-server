# Configuration

Precedence: flags, then `RATE_*` environment variables, then a YAML file, then
the defaults. The configuration is read at startup; restart after editing it.

## Flags

| Flag | Overrides |
|---|---|
| `-config path.yaml` | YAML file path. |
| `-port 9000` | `port` / `RATE_PORT`. |
| `-bind 127.0.0.1` | `bind` / `RATE_BIND`. |

## Keys

| YAML | Environment | Default | Description |
|---|---|---|---|
| `port` | `RATE_PORT` | `8080` | HTTP port. |
| `bind` | `RATE_BIND` | `""` | Bind address. `""` means all interfaces. |
| `sources` | `RATE_SOURCES` | all seven | Comma list: `ecb`, `erapi`, `tcmb`, `binance`, `coinbase`, `coingecko`, `fawazahmed`. |
| `fiat` | `RATE_FIAT` | `*` | `*` or a comma list. |
| `crypto` | `RATE_CRYPTO` | 12 assets | Comma list, or `none`. |
| `poll_market` | `RATE_POLL_MARKET` | `30s` | Binance and Coinbase interval. |
| `poll_tcmb` | `RATE_POLL_TCMB` | `1h` | TCMB interval. |
| `poll_fiat` | `RATE_POLL_FIAT` | `6h` | ECB and exchangerate-api interval. |
| `poll_coingecko` | `RATE_POLL_COINGECKO` | `5m` | CoinGecko interval. |
| `poll_fawaz` | `RATE_POLL_FAWAZ` | `12h` | fawazahmed0 interval. |
| `stale_market` | `RATE_STALE_MARKET` | `5m` | Market quote staleness threshold. |
| `stale_official` | `RATE_STALE_OFFICIAL` | `96h` | Official rate threshold; generous for weekends. |
| `stale_aggregator` | `RATE_STALE_AGGREGATOR` | `48h` | Aggregator threshold. |
| `strategy` | `RATE_STRATEGY` | `priority` | `priority`, `max`, `min`, `avg`, `median`. |
| `strategy_fiat` | `RATE_STRATEGY_FIAT` | inherits | Fiat pair override. |
| `strategy_crypto` | `RATE_STRATEGY_CRYPTO` | inherits | Crypto pair override. |
| `startup_timeout` | `RATE_STARTUP_TIMEOUT` | `15s` | Budget for the first parallel fetch. |
| `log_format` | `RATE_LOG_FORMAT` | `json` | `json` or `text`. |
| `log_level` | `RATE_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. |
| `cors` | `RATE_CORS` | `true` | `Access-Control-Allow-Origin: *`. |

Durations use Go syntax (`30s`, `5m`, `12h`). Lists accept either a YAML list or
a comma-separated string.

## Example file

```yaml
port: 9000
bind: 127.0.0.1
sources: [ecb, erapi, tcmb, binance, coinbase, coingecko, fawazahmed]
fiat: ["*"]
crypto: [BTC, ETH, USDT, USDC, SOL, XRP, ADA, DOGE, AVAX, DOT, BNB, LTC]
poll_market: 30s
poll_tcmb: 1h
poll_fiat: 6h
poll_coingecko: 5m
poll_fawaz: 12h
stale_market: 5m
stale_official: 96h
stale_aggregator: 48h
strategy: priority
strategy_fiat: ""
strategy_crypto: ""
startup_timeout: 15s
log_format: json
log_level: info
cors: true
```

`config.example.yaml` in the repository root is the same file with comments.

## Asset selection

`RATE_FIAT=*` enables every fiat currency in the embedded catalogue. To narrow
it, pass a comma list:

```bash
RATE_FIAT=USD,EUR,TRY,JPY go run ./cmd/rate-server
```

`RATE_CRYPTO=none` disables crypto. If crypto is the only asset class you want,
set `RATE_FIAT=none` and keep a crypto list. At least one of the two must be
non-empty.

## Polling and staleness

Official sources publish once per business day. Polling them more often only
re-downloads the same bulletin, so 1–6 hours is enough. Market sources and
CoinGecko benefit from shorter intervals; CoinGecko's free tier is rate-limited,
which is why it defaults to 5 minutes.

Staleness is per source class, not per provider. A quote older than its class
threshold is still served, but flagged as `stale`. That is intentional: a rate
from Friday is more useful on Sunday than a 503.

## Strategy

Strategy selection is documented in [aggregation.md](aggregation.md). The short
version: `priority` for an auditable, inverse-consistent rate; `median` for a
consensus that ignores one bad source; `max`/`min` for best/worst-case views.

## Logs and bind

`RATE_LOG_FORMAT=json` writes one object per request or provider event to
stdout. Use `text` when reading logs by hand.

The default bind is all interfaces. With no auth, prefer `RATE_BIND=127.0.0.1`
or a private network address, or place the server behind a reverse proxy.
