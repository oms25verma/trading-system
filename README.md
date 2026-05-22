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

Trades are persisted to `data/trades.json` by default, so local trade ids survive server restarts. Override the location with:

```bash
export TRADE_STORE_PATH=/path/to/trades.json
```

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

Use the returned `id` in the next calls.

Use the local `id`, not Kite's `entry_order_id`, for stop-loss, target, and exit APIs.

## Create a trade with automatic SL and target

Add a `protection` block to place stop-loss and target orders immediately after the entry order.

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
    "entry_order_id": "2057851784202215424"
  }'
```

## Add a stop-loss with trailing

```bash
curl -X POST http://localhost:8080/trades/<id>/stop-loss \
  -H 'Content-Type: application/json' \
  -d '{
    "trigger_price": 1490,
    "limit_price": 1489.95,
    "trail_by": 5
  }'
```

The service polls LTP every `POLL_SECONDS` and modifies the SL order when the price moves in your favor.

## Add a target

```bash
curl -X POST http://localhost:8080/trades/<id>/target \
  -H 'Content-Type: application/json' \
  -d '{"price": 1520}'
```

## Remove stop-loss or target

```bash
curl -X DELETE http://localhost:8080/trades/<id>/stop-loss
curl -X DELETE http://localhost:8080/trades/<id>/target
```

## Exit a trade

This cancels attached stop-loss and target orders, then places a market order in the opposite direction.

```bash
curl -X POST http://localhost:8080/trades/<id>/exit
```

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
- Target and stop-loss here are separate regular orders. If you need OCO behavior, implement GTT two-leg exits or cancel the sibling order when one completes by consuming order updates.
