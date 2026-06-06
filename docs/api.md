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

## Dashboard

```http
GET /dashboard
```

Returns a compact read-only summary for the first UI screen:

```json
{
  "risk_status": "CONFLICT",
  "active_groups": 1,
  "managed_groups": 0,
  "unmanaged_groups": 0,
  "conflict_groups": 1,
  "warning_groups": 0,
  "open_trades": 1,
  "closed_trades": 0,
  "open_orders": 2,
  "rejected_orders": 1,
  "synced_orders": 3,
  "synced_positions": 1,
  "warnings": {
    "AMBIGUOUS_EXTERNAL_STOP_LOSS": 1
  },
  "conflicts": [],
  "unmanaged_positions": [],
  "partially_managed": [],
  "recent_open_orders": [],
  "recent_rejected_orders": []
}
```

`risk_status` is `OK`, `WARNING`, or `CONFLICT`.

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
  "positions_added": 1,
  "positions_changed": 1,
  "positions_removed": 0,
  "product_conversions_detected": 0,
  "external_stop_losses_linked": 0,
  "external_targets_linked": 0,
  "ambiguous_external_exits": 0,
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

List endpoints support paged mode when any query parameter is supplied. Useful filters for orders:

```http
GET /orders?status=OPEN&creation_source=LOCAL_SYSTEM&page=1&page_size=50&sort_by=synced_at&sort_dir=desc
```

### List Synced Positions

```http
GET /positions
```

Returns the latest locally synced Kite net position snapshot.

Useful filters:

```http
GET /positions?exchange=MCX&symbol=SILVERM26JUNFUT&page=1&page_size=50
```

## Trades

### List Trades

```http
GET /trades
```

Without query parameters this returns the original plain JSON array. With query parameters it returns:

```json
{
  "data": [],
  "pagination": {
    "page": 1,
    "page_size": 50,
    "total": 0,
    "total_pages": 0,
    "has_next": false,
    "has_prev": false
  }
}
```

Useful filters:

```http
GET /trades?status=OPEN&side=BUY&product=MIS&page=1&page_size=50&sort_by=updated_at&sort_dir=desc
```

## Position Groups

### List Active Position Groups

```http
GET /groups
```

Groups merge open local trades with the latest synced Kite net positions and are keyed by `exchange + tradingsymbol + product`. Synced Kite-only positions appear as `UNMANAGED` so the UI can surface them before local SL/target management is added.

Useful filters:

```http
GET /groups?management_status=UNMANAGED&exchange=MCX&page=1&page_size=50&sort_by=tradingsymbol
```

Supported common list parameters:

```text
page, page_size, sort_by, sort_dir
exchange, symbol/tradingsymbol, side, product, status
creation_source, management_status, order_type, warning
```

`page_size` defaults to `50` and is capped at `500`.

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
    "local_quantity": 2,
    "broker_quantity": 2,
    "average_entry_price": 109000,
    "trade_ids": ["silverm26junfut-..."],
    "trade_status": "OPEN",
    "creation_source": "LOCAL_SYSTEM",
    "management_status": "MANAGED",
    "warnings": ["OPPOSITE_EXPOSURE_ACROSS_PRODUCTS"],
    "created_at": "2026-05-24T09:15:00Z",
    "updated_at": "2026-05-24T09:16:00Z"
  }
]
```

Creating a new opposite-side entry for an active group is rejected with `CONFLICT/opposite_side_active_group`; use the exit flow to reduce or close the existing position.

If the same symbol has opposite exposure across different products, for example `BUY MIS` and `SELL NRML`, groups include `OPPOSITE_EXPOSURE_ACROSS_PRODUCTS` in `warnings`.

If synced broker quantity differs from local child-trade quantity, broker quantity is shown as the active group `quantity`, `management_status` becomes `PARTIALLY_MANAGED`, and warnings identify partial external exits or other mismatches.

For an unambiguous group with exactly one open local trade, sync reconciliation applies external changes automatically:

- full external flatten: cancel remaining SL/target orders and close the local trade with `MANUAL_EXTERNAL`
- partial external exit: reduce local remaining `quantity`, preserve `initial_quantity`, and resize remaining SL/target order quantities

For groups with multiple open child trades, the service does not guess how to allocate a partial external exit. The group remains `PARTIALLY_MANAGED` until group-level allocation logic is added.

If sync detects a product conversion such as `MIS -> NRML`, the old and new groups include `PRODUCT_CONVERSION_DETECTED`, plus `converted_to_product` or `converted_from_product`. The local trade remains open, but add/modify SL, add/modify target, and exit actions using the stale product are rejected with `CONFLICT/product_conversion_detected` until product migration is implemented. Removing old protection orders remains allowed.

For an unambiguous group with exactly one open local trade, apply the detected migration with:

```http
POST /trades/{id}/product-conversion/apply
```

This cancels old-product SL/target orders, updates the local trade product and remaining quantity, clears the stale-product guard, and recreates existing protection orders under the new product. Multi-trade conversion remains blocked until group-level allocation is available.

### Group Actions

For a managed group with exactly one open linked local trade, the UI can use group ids such as `NSE:INFY:MIS`:

```http
POST   /groups/{id}/stop-loss
DELETE /groups/{id}/stop-loss
POST   /groups/{id}/target
DELETE /groups/{id}/target
POST   /groups/{id}/exit
POST   /groups/{id}/cancel-entry
```

Request bodies match the equivalent trade-level APIs. These endpoints delegate to the existing validated trade workflow.

External-only groups return `CONFLICT/group_unmanaged`. Groups with multiple open local child trades return `CONFLICT/ambiguous_group_trades` until take-over and group-allocation logic is implemented.

### Take Over External Position

For a synced Kite-only `UNMANAGED` group, create a local management record without placing a duplicate entry order:

```http
POST /groups/{id}/take-over
```

Optional body:

```json
{
  "entry_price": 100
}
```

If `entry_price` is omitted, the service uses broker LTP. After take-over, the normal group SL/target/exit APIs are available. Repeating take-over returns `CONFLICT/group_already_managed`.

During Kite sync, external SL/target orders are auto-linked to a managed local trade only when matching is unambiguous:

- same exchange, symbol, and product
- opposite transaction side
- open order
- matching quantity
- external source, not tagged `TSLOCAL`
- exactly one matching SL or target candidate

If multiple candidates exist, the system skips linking so the future UI can resolve the conflict manually.

Ambiguous external SL/target candidates are exposed on position groups:

```json
{
  "management_status": "CONFLICT",
  "warnings": ["AMBIGUOUS_EXTERNAL_STOP_LOSS"]
}
```

Resolve an ambiguous external exit by explicitly linking one synced Kite order:

```http
POST /groups/{id}/external-exit/link
Content-Type: application/json

{
  "order_id": "250603000000000",
  "role": "stop_loss"
}
```

Supported roles are `stop_loss` and `target`. The order must be open, external, opposite-side, same exchange/symbol/product, and match the group quantity. Linked orders are then modified by normal group SL/target APIs.

To clear only the local association without cancelling the Kite order:

```http
DELETE /groups/{id}/external-exit/link
Content-Type: application/json

{
  "order_id": "250603000000000",
  "role": "stop_loss"
}
```

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

For completed entries, cancels SL/target and places a market order in the opposite direction.

For open `LIMIT` entries that are not filled yet, this safely cancels the pending entry order instead of placing a reverse market order. The trade is closed locally with `exit_reason: "ENTRY_CANCELLED"`.

You can also explicitly cancel an unfilled entry order:

```http
POST /trades/{id}/cancel-entry
```

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
