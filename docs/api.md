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

## Trades

### List Trades

```http
GET /trades
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

Cancels SL/target and places a market order in the opposite direction.

## Current Background Behavior

- Polls every `POLL_SECONDS`.
- Applies pending protection after LIMIT entry completion.
- Reconciles Kite-side SL/target cancel/modify/complete.
- Runs OCO.
- Detects manual external position flattening.

## Future API Docs

- Add OpenAPI/Swagger spec.
- Add position-group APIs.
- Add Kite sync APIs.
- Add auto-square-off and kill-switch APIs.
- Add conflict-resolution APIs.
