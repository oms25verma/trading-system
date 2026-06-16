# Go Trading System Starter for Zerodha Kite

This is a small starter service for managing trades through a broker adapter. It supports:

- buy/sell entry orders
- add/remove stop-loss orders
- add/remove target orders
- active position groups from current local trades
- live position-group LTP and unrealized P&L view
- dashboard summary for counts, conflicts, warnings, and active orders
- conflict queue for groups that need operator attention
- orderbook cancellation for open synced orders
- metadata and OpenAPI endpoints for frontend bootstrap
- app-managed trailing stop-loss by polling LTP and modifying the stop order
- paper broker mode for local testing
- Kite HTTP adapter using Kite Connect v3 endpoints

## Quick Start

Requirements:

- Go `1.22+`
- Node `20.20.0` for the frontend (`web/.nvmrc`)
- Zerodha Kite Connect app only when using `BROKER=kite`

### 1. Configure Environment

Create a local `.env` file:

```bash
cp .env.example .env
```

For first local testing, keep:

```bash
BROKER=paper
HTTP_ADDR=:8080
TRADE_STORE_PATH=data
# SYMBOL_WATCHLIST_FILE=config/symbols.json
ENFORCE_SYMBOL_WATCHLIST=false
```

Load the file before running the backend:

```bash
set -a
source .env
set +a
```

You need to repeat the `source .env` step in every new terminal session, unless you put these exports in your shell profile.

### 2. Run Backend

```bash
GOCACHE=/private/tmp/trading-system-gocache go run ./cmd/server
```

The server starts on `http://127.0.0.1:8080`.

Check it:

```bash
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/metadata
curl http://127.0.0.1:8080/openapi.json
```

`/openapi.json` includes starter request/response schemas for the trade, group, orderbook, sync, and dashboard flows.

### 3. Run Frontend

In another terminal:

```bash
cd web
nvm use
npm install
npm run dev
```

Open:

```text
http://127.0.0.1:5173
```

The Vite dev server proxies `/api/*` to `http://127.0.0.1:8080`, so keep the Go backend running.

### 4. Optional: Use Kite Live Broker

In the Kite developer console, set the redirect URL to:

```text
http://127.0.0.1:8080/kite/callback
```

You do not need a public server for login. Kite redirects your browser back to your local machine.

Set these in `.env`:

```bash
BROKER=paper
KITE_API_KEY=your_api_key
KITE_API_SECRET=your_api_secret
```

Start the backend, then open:

```text
http://127.0.0.1:8080/kite/login
```

After Zerodha login, the browser returns to `/kite/callback` and prints `access_token`. Put that token into `.env`:

```bash
BROKER=kite
KITE_ACCESS_TOKEN=access_token_from_callback
```

Then reload env and restart the backend:

```bash
set -a
source .env
set +a
GOCACHE=/private/tmp/trading-system-gocache go run ./cmd/server
```

Kite access tokens are short-lived; expect to repeat the login/token step each trading day.

### 5. Kite Allowed IP For Order Placement

Kite live order placement requires the app's allowed IP list to include your current public IP. Check your public IP:

```bash
curl -4 https://ifconfig.me
curl -6 https://ifconfig.me
```

Add the IP shown in Kite developer console. If your ISP gives dynamic IPs, this may change frequently. For production, prefer a VPS/static IPv4 or a VPN/tunnel through a static-IP server.

If you see this error, your current IP is not whitelisted:

```text
IP (...) is not allowed to place orders for this app
```

### 6. Optional: Sync Kite Data And Instruments

Fetch Kite orders and positions:

```bash
curl -X POST http://127.0.0.1:8080/sync/kite
```

Fetch F&O instrument master for option dropdowns:

```bash
curl -X POST "http://127.0.0.1:8080/instruments/sync?exchange=NFO"
curl "http://127.0.0.1:8080/instruments/expiries?exchange=NFO&underlying=NIFTY"
curl "http://127.0.0.1:8080/instruments/options?exchange=NFO&underlying=NIFTY&range_points=1000&contracts_each_side=10"
```

Fetch MCX instrument master for commodity futures such as `GOLD`, `SILVER`, and `SILVERM`:

```bash
curl -X POST "http://127.0.0.1:8080/instruments/sync?exchange=MCX"
curl "http://127.0.0.1:8080/instruments/future-underlyings?exchange=MCX"
curl "http://127.0.0.1:8080/instruments/futures?exchange=MCX&underlying=SILVERM"
```

The New Trade drawer starts with an instrument selector. Choose `NFO Options`, `BFO Options`, or `MCX Futures`; the form then shows only the relevant controls.

For options, the form can sync `NFO` or `BFO`, list underlyings/expiries, show the selected underlying's LTP, and load contracts around ATM. The main UI hides manual center-strike and range controls for now; it uses the backend's ATM detection with a default `range_points=1000`. If LTP is unavailable, the backend falls back to a median strike so the dropdown still works. Option lot sizes are enforced in the UI and backend; quantities must be multiples of the contract lot size.

For MCX futures, sync `MCX`, choose a commodity, then load/select the futures contract. Lot size and tick size come from Kite's instrument master.

### 7. LTP And Market Data Permission

For LIMIT orders, the UI can fill the limit price with:

```http
GET /market/ltp?exchange=NFO&symbol=NIFTY2661623250CE
```

With Kite, this requires quote/LTP permission. If Kite returns:

```text
Insufficient permission for that call
```

enable the required market-data permission for the Kite app/account, or enter the LIMIT price manually.

The Positions page also uses:

```http
GET /market/groups
GET /positions/live
```

`/market/groups` returns the current position groups enriched with batched LTP, unrealized P&L, and P&L percent where market data is available. `/positions/live` reads the broker's current net positions directly, so Kite positions can appear in the UI even before local sync snapshots have been persisted. The frontend polls the backend every 5 seconds by default and has a Live/Paused toggle in the top bar. Polling calls your local backend only; the backend batches active symbols into Kite `/quote/ltp` requests when supported.

Kite's market quote docs currently state:

- `/quote` supports up to 500 instruments per request.
- `/quote/ohlc` supports up to 1000 instruments per request.
- `/quote/ltp` supports up to 1000 instruments per request.
- WebSocket streaming is the realtime quote path and supports up to 3000 instruments per connection, with up to 3 WebSocket connections per API key.

Docs:

- https://kite.trade/docs/connect/v3/market-quotes/
- https://kite.trade/docs/connect/v3/websocket/

For now this app uses conservative batched REST polling for the UI and keeps Kite WebSocket parked for the next realtime phase.

### Historical Data And Automation Starter

Automation and backtesting code lives separately under `internal/automation` so it does not mix with live trade-management logic.

Use Kite historical candles to sync a specific instrument/date range into local JSON storage:

```bash
curl -X POST http://127.0.0.1:8080/historical/sync \
  -H 'Content-Type: application/json' \
  -d '{
    "exchange": "MCX",
    "tradingsymbol": "SILVERM26JUNFUT",
    "instrument_token": 118822663,
    "interval": "minute",
    "from": "2026-06-01T09:00:00+05:30",
    "to": "2026-06-30T23:30:00+05:30",
    "include_oi": true
  }'
```

Candles are stored by month under:

```text
data/historical/<EXCHANGE>/<TRADINGSYMBOL>/<interval>_<YYYY_MM>.json
```

Supported starter intervals are `minute`, `3minute`, `5minute`, `10minute`, `15minute`, `30minute`, `60minute`, and `day`. Historical sync needs the correct `instrument_token`; expired F&O contracts need previously cached token metadata or another reliable token registry.

Search the latest cached Kite instrument master for a token before syncing:

```bash
curl "http://127.0.0.1:8080/historical/instruments?exchange=MCX&underlying=SILVERM&instrument_type=FUT&limit=25"
```

This search works from files created by `POST /instruments/sync?exchange=<EXCHANGE>`. It is good for current cached contracts. A durable token registry for expired F&O contracts is still a later database-backed task.

Read stored candles:

```bash
curl "http://127.0.0.1:8080/historical/candles?exchange=MCX&tradingsymbol=SILVERM26JUNFUT&interval=minute&from=2026-06-01T09:00:00%2B05:30&to=2026-06-30T23:30:00%2B05:30"
```

Run the starter opening-range breakout backtest:

```bash
curl -X POST http://127.0.0.1:8080/backtests \
  -H 'Content-Type: application/json' \
  -d '{
    "exchange": "MCX",
    "tradingsymbol": "SILVERM26JUNFUT",
    "interval": "minute",
    "from": "2026-06-01T09:00:00+05:30",
    "to": "2026-06-30T23:30:00+05:30",
    "strategy": "opening_range_breakout",
    "quantity": 1,
    "multiplier": 5,
    "stop_loss_points": 500,
    "target_points": 1000,
    "entry_buffer_points": 0,
    "slippage_points": 0.5,
    "brokerage_per_trade": 40,
    "range_start": "09:15",
    "range_end": "09:30",
    "exit_time": "15:20"
  }'
```

Backtest P&L uses `price_diff * quantity * multiplier`, then subtracts `brokerage_per_trade` from each completed trade. `slippage_points` worsens both entry and exit prices. Backtest results include gross P&L, total costs, net P&L, max drawdown, win rate, expectancy, average win/loss, and an equity curve. Results are stored under `data/backtests/`. Use `GET /backtests` to list summaries and `GET /backtests/<id>` for the full trade list.

The frontend has an `Automation` page for the same flow:

- sync historical candles
- search cached instruments and apply the token to sync/backtest forms
- run the opening-range breakout backtest
- view saved backtest summaries
- inspect the latest backtest trades, costs, expectancy, and equity curve

Kite MCP can be useful later as an AI assistant layer for portfolio/market analysis and strategy exploration. Zerodha's MCP article says MCP currently focuses on market data and portfolio analysis; order placement, historical trade data, portfolio data, and some other features are unavailable at this time. For this app, Kite Connect remains the execution and persistent-system integration path, while MCP is a possible future assistant interface over our own backend.

### 8. Logs And Persistence

Trades, synced orderbook snapshots, synced position snapshots, and instrument cache files are stored under `data/` by default. Examples:

```text
data/trades_24_05_2026.json
data/orders_24_05_2026.json
data/positions_24_05_2026.json
data/instruments_NFO_12_06_2026.json
```

Override storage with:

```bash
export TRADE_STORE_PATH=/path/to/trading-data
```

Logs are emitted as structured JSON to stdout. Set `LOG_LEVEL=debug|info|warn|error`.

Warning and error logs are also written to `ERROR_LOG_PATH` (`data/error.log` by default). That file is truncated every time the server starts, so it contains only the current run's actionable failures. Expected validation rejects and Kite `InputException` order rejects stay in stdout with request ids, but are not copied into the error file.

Every request gets an `X-Request-ID`; pass your own or use the generated response header to filter correlated logs. Kite broker request/response metadata is logged at `debug` level with sensitive fields redacted.

## Configuration Notes

Watchlist-based order entry is currently parked. The app now prefers Kite instrument-master sync for option contracts because it gives current expiries, strikes, lot sizes, and tick sizes without manually maintaining many symbols.

The old watchlist config is still documented here for later reuse, but backend symbol rejection is intentionally disabled while dynamic Kite instruments are the primary order-entry source:

```bash
# export SYMBOL_WATCHLIST_FILE=config/symbols.json
export ENFORCE_SYMBOL_WATCHLIST=false
```

Example:

```json
{
  "symbols": [
    {
      "exchange": "MCX",
      "symbol": "SILVERM26JUNFUT",
      "product": "MIS",
      "name": "Silver Mini Jun Fut",
      "default_quantity": 1,
      "lot_size": 1,
      "tick_size": 1
    }
  ]
}
```

You can also use `"products": ["MIS", "NRML"]` to show the same instrument for multiple products, or `"enabled": false` to keep expired contracts in the file without showing them. The older quick env format still works when `SYMBOL_WATCHLIST_FILE` is empty:

```bash
export SYMBOL_WATCHLIST=NSE:INFY:MIS,MCX:SILVERM26JUNFUT:MIS
```

Lot-size validation still uses synced Kite instruments where available, so run `POST /instruments/sync?exchange=NFO`, `BFO`, or `MCX` before placing dynamic F&O or commodity futures orders.

Set `REQUIRE_ORDER_PROTECTION=true` when you want every new order request to include a `protection` block with both SL and target points. Defaults fill zero values inside the block, but the caller must intentionally request protection.

Orders created by this service are sent to Kite with tag `TSLOCAL`. This is used by the sync engine to distinguish local-system orders from Kite app/manual orders.

## Create a trade

```bash
curl -X POST http://localhost:8080/trades \
  -H 'Content-Type: application/json' \
  -d '{
    "exchange": "NSE",
    "tradingsymbol": "INFY",
    "side": "BUY",
    "quantity": 1,
    "product": "MIS",
    "order_type": "MARKET",
    "market_protection": -1
  }'
```

`market_protection` is required by Kite for `MARKET` and `SL-M` orders. Use `-1` for automatic protection, or use a percentage such as `2` for 2%.

Kite Connect is not MIS-only. This starter defaults to `MIS` when `product` is omitted, but you can pass the product allowed for the segment, such as:

```text
MIS  - intraday
CNC  - equity delivery
NRML - futures/options/commodity carry-forward where allowed
```

Kite also provides a position conversion API for converting open positions between allowed products such as MIS and NRML/CNC, subject to exchange, product, margin, and RMS rules.

Use the returned `id` in the next calls.

Use the local `id`, not Kite's `entry_order_id`, for stop-loss, target, and exit APIs.

Use `GET /groups` to see active position groups aggregated by `exchange + tradingsymbol + product`. Groups merge local trades with synced Kite net positions, so Kite-only positions appear as `UNMANAGED` and quantity mismatches appear as `PARTIALLY_MANAGED` warnings. The service rejects a new opposite-side entry when an active group already exists for the same key; use the exit flow for reducing/closing that position.

For a group with exactly one open local trade, sync reconciliation automatically closes the trade after an external flatten or reduces its remaining quantity after a partial external exit. Existing SL/target order quantities are resized for the remaining position. Multi-trade partial allocation is intentionally left for the group-management layer.

Sync also detects unambiguous product conversions such as `MIS -> NRML`. Converted groups are flagged, and stale-product SL/target/exit actions are blocked until you call `POST /trades/{id}/product-conversion/apply`. For a single-trade group, that endpoint migrates the local product and recreates existing protection orders under the new product.

For a managed group with one linked local trade, UI actions can use `/groups/{id}/stop-loss`, `/groups/{id}/target`, `/groups/{id}/exit`, and `/groups/{id}/cancel-entry`. External-only and multi-trade groups return explicit conflicts until take-over and group-allocation workflows are added.

Use `POST /groups/{id}/take-over` for a synced external-only position. It creates a local management record without placing another entry order, then enables the normal group SL/target/exit APIs.

Kite sync auto-links external SL/target orders to a managed trade only when there is exactly one clear match. Ambiguous matches are surfaced as `CONFLICT` groups with warnings such as `AMBIGUOUS_EXTERNAL_STOP_LOSS`; use `POST /groups/{id}/external-exit/link` to manually choose the order to manage.

When `entry_price` is known, the service validates risk orders locally before sending them to Kite:

```text
BUY trade  -> stop-loss below entry, target above entry
SELL trade -> stop-loss above entry, target below entry
```

## Create a trade with automatic SL and target

Add a `protection` block to place stop-loss and target orders for the entry.

In the frontend this is shown as `Est. Entry Price` for `MARKET` orders. It is the risk basis used to preview SL/target before the actual fill is known. In the API the field is still named `reference_price`.

For `LIMIT` orders, the limit price itself is used as the risk basis. The frontend hides the extra estimated-entry input in that case so the preview follows the editable limit price.

For option premiums, the frontend adapts suggested SL/target points to the entry basis. This prevents low-premium contracts, such as an option trading near `25`, from getting a default `20` point SL that would create a negative stop price.

Frontend protection presets are instrument-aware and remain editable after selection:

```text
NFO/BFO options -> 5% / 10%, 10% / 15%, 15% / 25%
MCX futures     -> 0.10% / 0.20%, 0.20% / 0.30%, 0.30% / 0.50%
```

MCX uses tighter defaults because high-price futures such as `SILVERM` would otherwise produce very large intraday risk and target distances.

Risk/reward preview uses this formula:

```text
estimated P&L = price points × order quantity × P&L multiplier
```

For NFO/BFO options and most equity-style instruments, Kite quantity already represents the lot-adjusted quantity, so the multiplier is `1`. For MCX futures, the frontend applies a commodity P&L multiplier when known. This multiplier is separate from Kite's instrument `lot_size`, which may be `1` for MCX contracts.

Current MCX multipliers covered in the frontend preview:

```text
ALUMINIUM 5000   ALUMINI 1000      COPPER 2500
LEAD 5000        LEADMINI 1000     NICKEL 1500
ZINC 5000        ZINCMINI 1000
GOLD 100         GOLDM 10          GOLDPETAL 1
GOLDGUINEA 1     GOLDTEN 1
SILVER 30        SILVERM 5         SILVERMIC 1
SILVER100 0.1
CRUDEOIL 100     CRUDEOILM 10
NATURALGAS 1250  NATGASMINI 250
```

Example: `SILVERM` uses multiplier `5`, so a `1000` point move on quantity `1` previews approximately `5000` profit/loss. Unknown MCX roots fall back to multiplier `1` until verified.

```bash
curl -X POST http://localhost:8080/trades \
  -H 'Content-Type: application/json' \
  -d '{
    "exchange": "MCX",
    "tradingsymbol": "SILVERM26JUNFUT",
    "side": "SELL",
    "quantity": 1,
    "product": "MIS",
    "order_type": "MARKET",
    "market_protection": -1,
    "protection": {
      "reference_price": 109000,
      "stop_loss_points": 20,
      "target_points": 40,
      "trail_by": 10,
      "sl_limit_offset": 5
    }
  }'
```

For a `SELL` trade at `109000`, this creates:

```text
stop-loss trigger = 109020
stop-loss limit   = 109025
target            = 108960
```

For a `BUY` trade, the same points are applied in the opposite direction. For `MARKET` orders, pass `reference_price` if you want deterministic SL/target prices; otherwise the service tries to use LTP. `trail_by` is optional; when set, the backend trailing-stop poller moves the SL in your favor by that point gap while the position is active.

If `sl_limit_offset` is omitted, defaults are currently:

```text
MCX commodities: 10.0
Other exchanges: 0.05
```

These constants live in `internal/trading/constants.go` and can later move into production config.

For `LIMIT` entry orders, protection is saved as `pending_protection` and is not sent to Kite immediately. The background poller checks the entry order status every `POLL_SECONDS`; once the entry is `COMPLETE`, it creates the stop-loss and target orders.

## Import an existing trade

Use this after a server restart if the trade was created before persistence was enabled. This does not place a new entry order; it only stores the local trade record.

```bash
curl -X POST http://localhost:8080/trades/import \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "silverm26junfut-1779465072440087000",
    "exchange": "MCX",
    "tradingsymbol": "SILVERM26JUNFUT",
    "side": "SELL",
    "quantity": 1,
    "product": "MIS",
    "entry_price": 109000,
    "entry_order_id": "2057851784202215424"
  }'
```

## Set a stop-loss with trailing

```bash
curl -X POST http://localhost:8080/trades/<id>/stop-loss \
  -H 'Content-Type: application/json' \
  -d '{
    "trigger_price": 1490,
    "limit_price": 1489.95,
    "trail_by": 5
  }'
```

If no stop-loss exists, this creates one. If a stop-loss already exists, this modifies the existing Kite order. The service polls LTP every `POLL_SECONDS` and modifies the SL order when the price moves in your favor.

For an open `LIMIT` entry, this saves `pending_stop_loss` locally and does not send the SL order to Kite until the entry order is `COMPLETE`.

## Set a target

```bash
curl -X POST http://localhost:8080/trades/<id>/target \
  -H 'Content-Type: application/json' \
  -d '{"price": 1520}'
```

If no target exists, this creates one. If a target already exists, this modifies the existing Kite order.

For an open `LIMIT` entry, this saves `pending_target` locally and does not send the target order to Kite until the entry order is `COMPLETE`.

## Remove stop-loss or target

```bash
curl -X DELETE http://localhost:8080/trades/<id>/stop-loss
curl -X DELETE http://localhost:8080/trades/<id>/target
```

These APIs cancel the existing Kite SL/target order and clear the local saved state.

For an open `LIMIT` entry, these APIs clear `pending_stop_loss` or `pending_target` locally. No Kite cancel call is made because no SL/target order has been sent yet.

## Exit a trade

For completed entries, this cancels attached stop-loss and target orders, then places a market order in the opposite direction. For open `LIMIT` entries that are not filled yet, it cancels the pending entry and closes the local trade with `exit_reason` set to `ENTRY_CANCELLED`.

```bash
curl -X POST http://localhost:8080/trades/<id>/exit
```

You can also cancel an unfilled entry explicitly:

```bash
curl -X POST http://localhost:8080/trades/<id>/cancel-entry
```

## OCO behavior

When both stop-loss and target orders exist, the poller checks their Kite order statuses every `POLL_SECONDS`.

```text
SL COMPLETE     -> cancel target, close trade with exit_reason STOP_LOSS
Target COMPLETE -> cancel SL, close trade with exit_reason TARGET
Both COMPLETE   -> close trade with exit_reason BOTH_COMPLETED
```

Once a trade is closed, set stop-loss, set target, and manual exit APIs are rejected. The polling implementation checks locally tracked open trades that have SL and/or target order ids; trades without exit orders are skipped for OCO status checks.

The poller also reconciles changes made directly in Kite:

```text
SL/target CANCELLED or REJECTED in Kite -> clear local SL/target state
SL/target modified in Kite              -> update local price/trigger values
Position manually flattened in Kite     -> cancel remaining exits and close trade with exit_reason MANUAL_EXTERNAL
```

Position reconciliation uses Kite net positions for the trade's exchange, symbol, and product.

## Kite Operational Notes

- Use the Quick Start section for the full Kite setup flow.
- Redirect URL for local development is `http://127.0.0.1:8080/kite/callback`.
- Generate a fresh `KITE_ACCESS_TOKEN` through `/kite/login` when the token expires, usually daily.
- Live order placement requires your current public IP in Kite developer console's allowed IP list.
- If your ISP IP changes often, use a VPS/static IPv4 or VPN/tunnel through a static-IP server.
- LTP/quote endpoints require Kite market-data permission; otherwise enter LIMIT prices manually.
- F&O contract dropdowns require `POST /instruments/sync?exchange=NFO` or `BFO` after Kite credentials are configured.
- MCX futures dropdowns require `POST /instruments/sync?exchange=MCX`.

## Notes

- Be careful with live orders. Test paper mode first.
- Kite regular stop-loss orders are day-valid; for longer-lived exits, add a GTT adapter.
- Target and stop-loss here are separate regular orders. The starter provides polling-based OCO behavior; a Kite postback/webhook can be added later for faster production reconciliation.
