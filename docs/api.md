# API Documentation

Base URL:

```text
http://localhost:8080
```

## Error Format

```json
{
  "kind": "VALIDATION",
  "code": "invalid_quantity",
  "message": "quantity must be positive"
}
```

Common error kinds:

```text
VALIDATION
NOT_FOUND
CONFLICT
CLOSED
BROKER
```

## Health

```http
GET /healthz
```

## Kite Auth

```http
GET /kite/login
GET /kite/callback?request_token=...
```

`/kite/login` redirects to Kite. `/kite/callback` exchanges `request_token` for `access_token`.

Orders placed through this API are tagged as `TSLOCAL` when sent to Kite.

## Kite Sync

### Sync Kite Data

```http
POST /sync/kite
```

Fetches the current Kite orderbook and net positions through the configured broker and stores the latest snapshots locally. With directory storage, snapshots are saved as `orders_DD_MM_YYYY.json` and `positions_DD_MM_YYYY.json`.

This endpoint is always available as a force refresh. Automatic background sync is disabled by default; set `SYNC_POLL_SECONDS` to a positive value to enable the sync poller.

```json
{
  "synced_at": "2026-05-28T10:00:00Z",
  "orders_synced": 3,
  "positions_synced": 2,
  "local_system_orders": 1,
  "external_orders": 2
}
```

### List Synced Orders

```http
GET /orders
```

Returns the latest locally synced orderbook snapshot. `creation_source` is inferred from Kite `tag`:

```text
TSLOCAL   -> LOCAL_SYSTEM
empty tag -> KITE_APP
other tag -> UNKNOWN
```

### List Synced Positions

```http
GET /positions
```

Returns the latest locally synced Kite net position snapshot.

## Trades

### List Trades

```http
GET /trades
```

## Position Groups

### List Active Position Groups

```http
GET /groups
```

Groups are currently derived from open local trades and keyed by `exchange + tradingsymbol + product`.

Example response:

```json
[
  {
    "id": "MCX:SILVERM26JUNFUT:MIS",
    "exchange": "MCX",
    "tradingsymbol": "SILVERM26JUNFUT",
    "product": "MIS",
    "side": "SELL",
    "quantity": 2,
    "average_entry_price": 109000,
    "trade_ids": ["silverm26junfut-..."],
    "trade_status": "OPEN",
    "warnings": ["OPPOSITE_EXPOSURE_ACROSS_PRODUCTS"],
    "created_at": "2026-05-24T09:15:00Z",
    "updated_at": "2026-05-24T09:16:00Z"
  }
]
```

Creating a new opposite-side entry for an active group is rejected with `CONFLICT/opposite_side_active_group`; use the exit flow to reduce or close the existing position.

If the same symbol has opposite exposure across different products, for example `BUY MIS` and `SELL NRML`, groups include `OPPOSITE_EXPOSURE_ACROSS_PRODUCTS` in `warnings`.

### Create Trade

```http
POST /trades
```

```json
{
  "exchange": "MCX",
  "tradingsymbol": "SILVERM26JUNFUT",
  "side": "SELL",
  "quantity": 1,
  "product": "MIS",
  "order_type": "MARKET",
  "market_protection": -1
}
```

Runtime defaults are applied when omitted:

```text
quantity          -> DEFAULT_QUANTITY
product           -> DEFAULT_PRODUCT
market_protection -> DEFAULT_MARKET_PROTECTION
```

### Create Trade With Protection

```json
{
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
}
```

If `protection` is present, missing point/offset values can be filled from:

```text
DEFAULT_STOP_LOSS_POINTS
DEFAULT_TARGET_POINTS
DEFAULT_SL_LIMIT_OFFSET
```

For open `LIMIT` entries, protection is saved as pending and sent only after the entry order is complete.

### Import Existing Trade

```http
POST /trades/import
```

```json
{
  "id": "silverm26junfut-1779465072440087000",
  "exchange": "MCX",
  "tradingsymbol": "SILVERM26JUNFUT",
  "side": "SELL",
  "quantity": 1,
  "product": "MIS",
  "entry_price": 109000,
  "entry_order_id": "2057851784202215424"
}
```

This stores a local trade record without placing a new Kite order.

## Stop-Loss

### Set Stop-Loss

```http
POST /trades/{id}/stop-loss
```

```json
{
  "trigger_price": 109020,
  "limit_price": 109030,
  "trail_by": 10
}
```

If no stop-loss exists, this creates one. If one exists, this modifies it. For open `LIMIT` entries, this stores `pending_stop_loss` until the entry completes.

### Remove Stop-Loss

```http
DELETE /trades/{id}/stop-loss
```

Cancels live Kite SL orders or clears pending local SL state.

## Target

### Set Target

```http
POST /trades/{id}/target
```

```json
{
  "price": 108960
}
```

If no target exists, this creates one. If one exists, this modifies it. For open `LIMIT` entries, this stores `pending_target` until the entry completes.

### Remove Target

```http
DELETE /trades/{id}/target
```

Cancels live Kite target orders or clears pending local target state.

## Manual Exit

```http
POST /trades/{id}/exit
```

Cancels SL/target and places a market order in the opposite direction.

## Current Background Behavior

- Polls every `POLL_SECONDS`.
- Applies pending protection after LIMIT entry completion.
- Reconciles Kite-side SL/target cancel/modify/complete.
- Runs OCO.
- Detects manual external position flattening.

## Logging

The server writes structured JSON logs to stdout. Configure level with:

```bash
export LOG_LEVEL=info
```

Supported levels:

```text
debug
info
warn
error
```

Lifecycle events include trade creation/import, SL/target create/modify/remove, manual exit, OCO/external close, reconciliation events, API errors, broker selection, and server startup.

## Request Correlation

Every HTTP request gets a request id. You can pass one:

```bash
curl -H 'X-Request-ID: my-debug-id-001' http://localhost:8080/trades
```

If omitted, the server generates one and returns it:

```text
X-Request-ID: <generated-id>
```

Logs include `request_id`, so you can filter all API, manager lifecycle, and Kite broker logs for one request. Broker request/response metadata is logged at `debug` level and sensitive fields are redacted.

## Future API Docs

- Add OpenAPI/Swagger spec.
- Add position-group APIs.
- Add Kite sync APIs.
- Add auto-square-off and kill-switch APIs.
- Add conflict-resolution APIs.
