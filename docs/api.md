# API

Base URL: `http://localhost:8080`. Every endpoint is `GET`; there is no auth and
no write path.

## `GET /v1/latest`

| Parameter | Default | Description |
|---|---|---|
| `base` | `USD` | Base asset. Known aliases such as `XBT` are normalized. |
| `symbols` | all enabled | Comma-separated targets, or `*`. The limit is 500. |
| `amount` | `1` | Multiplies every returned value. Must be positive. |
| `detail` | off | `1`, `true` or `yes`. |
| `precision` | off | Round values to 0–18 decimals. |
| `kind` | none | `fiat` or `crypto` when `symbols` expands. |

Plain response:

```json
{
  "amount": 1,
  "base": "USD",
  "date": "2026-11-16",
  "rates": {
    "EUR": 0.9079353550027238,
    "TRY": 50.37455
  },
  "stale": false
}
```

`rates` values are `rate × amount`.

Detail response:

```json
{
  "amount": 1,
  "base": "USD",
  "as_of": "2026-11-17T14:07:20Z",
  "date": "2026-11-16",
  "stale": false,
  "rates": {
    "TRY": {
      "rate": 50.37455,
      "value": 50.37455,
      "source": "tcmb",
      "class": "official",
      "rate_date": "2026-11-17",
      "path": ["USD", "TRY"],
      "age_seconds": 4,
      "stale": false
    }
  }
}
```

- `rate` is quote units per 1 base unit; `value` is `rate × amount`.
- `date` is the oldest bulletin date on the path; `as_of` is the oldest fetch
  time. A cross-rate is never fresher than its oldest leg.
- `stale` is per rate in detail mode and becomes top-level `true` if any rate is
  stale. Thresholds are class-based: 5 minutes for market quotes, 96 hours for
  official rates, 48 hours for aggregators.
- `path` is the graph path, for example `["BTC","USDT","USD"]`.
- `contributors` appears for `max`, `min`, `avg` and `median` when more than one
  source took part (see [aggregation.md](aggregation.md)).
- Symbols with no path are listed in `missing`; the request returns 200 if at
  least one symbol resolved and 404 if none did.

## Other endpoints

| Endpoint | Returns |
|---|---|
| `GET /v1/currencies?kind=fiat\|crypto` | `{"count":N,"assets":[{"code","name","kind","decimals"}]}` |
| `GET /v1/sources` | Readiness, snapshot version, active strategy and per-provider health. |
| `GET /healthz` | `{"status":"ok"}` while the process runs. |
| `GET /readyz` | 200 once at least one quote is loaded, 503 before that. |

`/v1/sources` entry fields: `name`, `class`, `interval`, `last_attempt`,
`last_success`, `last_error`, `consecutive_failures`, `retry_after`, `next_run`
and `edges`.

## HTTP behaviour

- `ETag` and `If-None-Match` return 304 when the snapshot and query are unchanged.
- `Cache-Control: public, max-age=5`.
- CORS is `*` by default; set `RATE_CORS=false` to turn it off.
- Errors:

```json
{"error":{"code":"unknown_asset","message":"unknown or disabled asset \"NOPE\"","param":"symbols"}}
```

Codes: `invalid_request`, `unknown_asset`, `no_rate_available`, `not_ready`,
`internal`. Status codes: 400 invalid input, 404 no path, 503 no data yet,
500 internal.
