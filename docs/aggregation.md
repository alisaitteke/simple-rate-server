# Aggregation

When several sources quote the same pair, one strategy decides how those quotes
become a single graph edge. The strategy is applied per pair, not to the final
cross-rate: a two-hop answer is the product of the selected quote for each hop.

| Strategy | Behaviour | Inverse-consistent |
|---|---|---|
| `priority` (default) | Best source rank wins; ties go to the fresher quote. | Yes |
| `max` | Highest quote in the requested direction. | No |
| `min` | Lowest quote in the requested direction. | No |
| `avg` | Arithmetic mean of all source quotes. | No |
| `median` | Median of all source quotes. | No |

Aliases: `highest`, `high` and `maximum` for `max`; `lowest`, `low` and
`minimum` for `min`; `average` and `mean` for `avg`.

## Configuration

```bash
# Global
RATE_STRATEGY=median go run ./cmd/rate-server

# Fiat uses median, crypto uses the highest quote
RATE_STRATEGY=priority RATE_STRATEGY_FIAT=median RATE_STRATEGY_CRYPTO=max go run ./cmd/rate-server
```

```yaml
strategy: priority
strategy_fiat: median
strategy_crypto: max
```

An empty per-kind value inherits the global setting. A single-source pair is
unaffected: `avg` and `median` return the original source and class instead of a
synthetic aggregate.

## What the numbers mean

Assume two sources quote USD/EUR: ECB at `0.90` and Coinbase at `0.91`.

| Strategy | USD/EUR | Source | Contributors |
|---|---|---|---|
| `priority` | `0.90` | `ecb` | none |
| `max` | `0.91` | `coinbase` | `coinbase`, `ecb` |
| `min` | `0.90` | `ecb` | `coinbase`, `ecb` |
| `avg` | `0.905` | `avg` | `coinbase`, `ecb` |
| `median` | `0.905` | `median` | `coinbase`, `ecb` |

For `avg` and `median`, `class` is `aggregator` and the dates come from the
oldest contributing quote. For `max` and `min`, the winning source's class,
`rate_date` and fetch time are kept. `detail=1` exposes `contributors` when
more than one source took part.

## Direction and inverse consistency

`max`, `min`, `avg` and `median` are direction-aware. `USD/TRY=max` returns the
highest USD/TRY quote. `TRY/USD=max` returns the highest TRY/USD quote, which is
the inverse of the lowest USD/TRY quote. That is the same asymmetry as a bid/ask
spread, and it means `rate(A,B) × rate(B,A)` is not exactly 1.

Use `priority` when you need inverse consistency, a single audit trail, or a
number you can tie back to one official bulletin. It is the default for that
reason.

## Which one to use

- `priority`: regulated or reference rates, accounting, anything that needs a
  named source.
- `median`: a consensus view that resists one bad source. Good default for
  dashboards.
- `avg`: smooth consensus when the sources are expected to be equally reliable.
- `max` / `min`: best or worst case, "best execution" views, and debugging
  spread between sources. Expect directional asymmetry.

## Response example

```bash
RATE_STRATEGY=avg go run ./cmd/rate-server
curl 'localhost:8080/v1/latest?base=USD&symbols=EUR&detail=1'
```

```json
{
  "rate": 0.9088363405564635,
  "value": 0.9088363405564635,
  "source": "avg",
  "class": "aggregator",
  "rate_date": "2026-11-16",
  "path": ["USD", "EUR"],
  "age_seconds": 9,
  "stale": false,
  "contributors": ["coinbase", "ecb", "erapi"]
}
```

The active policy is also echoed by `/v1/sources`:

```json
"strategy": {"fiat": "avg", "crypto": "priority"}
```
