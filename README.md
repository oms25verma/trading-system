# Go Trading System Starter for Zerodha Kite

This is a small starter service for managing trades through a broker adapter. It supports:

- buy/sell entry orders
- add/remove stop-loss orders
- add/remove target orders
- app-managed trailing stop-loss by polling LTP and modifying the stop order
- paper broker mode for local testing
- Kite HTTP adapter using Kite Connect v3 endpoints

## Run locally

```bash
go run ./cmd/server
```

The server starts on `:8080` and uses the paper broker by default.

```bash
curl http://localhost:8080/healthz
```

Trades are persisted date-wise by default, for example `data/trades_24_05_2026.json`, so local trade ids survive server restarts on the same trading day. Override the directory or exact file with:

```bash
export TRADE_STORE_PATH=/path/to/trading-data
# or
export TRADE_STORE_PATH=/path/to/trades_24_05_2026.json
```

See [.env.example](.env.example) for runtime configuration defaults and [docs/api.md](docs/api.md) for API examples.

Logs are emitted as structured JSON to stdout. Set `LOG_LEVEL=debug|info|warn|error`.

Every request gets an `X-Request-ID`; pass your own or use the generated response header to filter correlated logs. Kite broker request/response metadata is logged at `debug` level with sensitive fields redacted.

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

When `entry_price` is known, the service validates risk orders locally before sending them to Kite:

```text
BUY trade  -> stop-loss below entry, target above entry
SELL trade -> stop-loss above entry, target below entry
```

## Create a trade with automatic SL and target

Add a `protection` block to place stop-loss and target orders for the entry.

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

For a `BUY` trade, the same points are applied in the opposite direction. For `MARKET` orders, pass `reference_price` if you want deterministic SL/target prices; otherwise the service tries to use LTP.

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

This cancels attached stop-loss and target orders, then places a market order in the opposite direction.

```bash
curl -X POST http://localhost:8080/trades/<id>/exit
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

## Use Kite

For local development, set this as the redirect URL in the Kite developer console:

```text
http://127.0.0.1:8080/kite/callback
```

You do not need a public server for this login flow. Kite will redirect your browser back to your local machine after login.

Set these env vars before starting:

```bash
export KITE_API_KEY=your_api_key
export KITE_API_SECRET=your_api_secret
export POLL_SECONDS=5
go run ./cmd/server
```

Then open:

```text
http://127.0.0.1:8080/kite/login
```

After Zerodha login, the browser returns to `/kite/callback`, exchanges the `request_token`, and prints the `access_token`.

For live order placement, restart with:

```bash
export BROKER=kite
export KITE_API_KEY=your_api_key
export KITE_API_SECRET=your_api_secret
export KITE_ACCESS_TOKEN=access_token_from_callback
go run ./cmd/server
```

## Notes

- Be careful with live orders. Test paper mode first.
- Kite regular stop-loss orders are day-valid; for longer-lived exits, add a GTT adapter.
- Target and stop-loss here are separate regular orders. The starter provides polling-based OCO behavior; a Kite postback/webhook can be added later for faster production reconciliation.
