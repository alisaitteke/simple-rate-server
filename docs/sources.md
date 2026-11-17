# Sources

All seven sources are keyless. Coverage changes with the upstream; the server
filters every response to the enabled asset catalogue.

| Source | Class | Default poll | Base | Coverage | Notes |
|---|---|---|---|---|---|
| `ecb` | official | 6h | EUR | ~30 fiat | Daily reference XML, published on business days. |
| `tcmb` | official | 1h | TRY | ~20 fiat | Buy/sell midpoint. Normalizes `Unit=100` for JPY. |
| `binance` | market | 30s | USDT/USDC/EUR/TRY/BTC and other fiat pairs | 12 crypto | Symbol list from `exchangeInfo` once a day, then targeted ticker calls. |
| `coinbase` | market | 30s | USD | fiat and crypto | One `exchange-rates` request per poll. |
| `coingecko` | aggregator | 5m | USD/EUR/TRY | 12 crypto | Keyless free tier, rate-limited, treated as a fallback. |
| `erapi` | aggregator | 6h | USD | 160+ fiat | `open.er-api.com` free endpoint. |
| `fawazahmed` | aggregator | 12h | USD | broad fiat and crypto | Static dataset on jsDelivr with a Cloudflare Pages fallback. |

## What each source actually returns

- **ECB** publishes a daily EUR-based XML bulletin. It is the reference for most
  European pairs and the default choice for `EUR/USD`.
- **TCMB** publishes TRY per unit of foreign currency. Some rows use
  `Unit=100` (JPY, for example), so the provider divides by the unit before
  creating an edge. Buy and sell are averaged; a missing side falls back to the
  other.
- **Binance** downloads `exchangeInfo` once a day (about 18 MB) to learn which
  configured symbols are tradable, then requests only those tickers every 30
  seconds. If the targeted request fails, it falls back to the full ticker list
  and refreshes the symbol catalogue on the next cycle.
- **Coinbase** returns one USD-based table containing both fiat and crypto. It
  is the usual second source for crypto and the bridge for `USDT/USD` and
  `USDC/USD`.
- **CoinGecko** is asked for all configured coins in one call against USD, EUR
  and TRY. The free API has a low rate limit, so it runs every five minutes and
  sits below the venue feeds in the crypto priority list.
- **exchangerate-api** (`erapi`) covers the long tail of fiat currencies. It is
  an aggregator, not an official reference, so it ranks below ECB and TCMB.
- **fawazahmed0** is a static JSON dataset on jsDelivr, with a Cloudflare Pages
  mirror. It fills gaps for exotic currencies and provides another crypto
  fallback. The two URLs are tried in order.

## Failure behaviour

Each provider has its own schedule, exponential backoff and `Retry-After`
handling. A failed fetch does not remove the last good quote. The pair keeps
resolving from other sources, and the quote is marked `stale` once it passes its
class threshold. `/v1/sources` shows the last error, consecutive failures and
the next run.

If Binance is blocked in your region, the crypto graph still works through
Coinbase, CoinGecko and fawazahmed. If TCMB is down, TRY pairs fall back to
erapi, Coinbase and fawazahmed. This is the reason for seven sources.

## Traffic and storage

With the default intervals, expect roughly 50–100 MB/day of upstream traffic.
Binance's daily `exchangeInfo` is the largest single download. The server keeps
no files; quotes live in memory and disappear on restart.

## Attribution and data terms

The code is MIT-licensed. The data is not. Check each provider before exposing
this server outside your organisation:

- **ECB**: reference rates, attribution required.
- **TCMB**: subject to the Central Bank of the Republic of Türkiye's terms.
- **exchangerate-api** (`open.er-api.com`): free endpoint, attribution required.
- **CoinGecko**: free API, attribution required and rate-limited.
- **Binance / Coinbase**: public market data, subject to their API terms.
- **fawazahmed0**: check the upstream repository's licence; the dataset is
  assembled from multiple sources.

Running the server internally for your own applications is the intended use.
Turning it into a public rate API means checking every source's terms first.
