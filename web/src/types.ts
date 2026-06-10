export type Side = 'BUY' | 'SELL';
export type TradeStatus = 'OPEN' | 'CLOSED';
export type OrderStatus = 'OPEN' | 'COMPLETE' | 'CANCELLED' | 'REJECTED';
export type ManagementStatus = 'MANAGED' | 'UNMANAGED' | 'PARTIALLY_MANAGED' | 'CONFLICT';
export type RiskStatus = 'OK' | 'WARNING' | 'CONFLICT';

export interface StopLoss {
  trigger_price: number;
  limit_price: number;
  trail_by?: number;
  highest_ltp?: number;
  lowest_ltp?: number;
}

export interface Target {
  price: number;
}

export interface ManagedTrade {
  id: string;
  exchange: string;
  tradingsymbol: string;
  side: Side;
  quantity: number;
  initial_quantity?: number;
  product: string;
  entry_price?: number;
  entry_order_id: string;
  entry_status?: OrderStatus | string;
  trade_status?: TradeStatus | string;
  creation_source?: string;
  exit_reason?: string;
  exit_order_id?: string;
  stop_order_id?: string;
  stop_order_status?: OrderStatus | string;
  target_order_id?: string;
  target_order_status?: OrderStatus | string;
  stop_loss?: StopLoss;
  target?: Target;
  pending_protection?: unknown;
  pending_stop_loss?: StopLoss;
  pending_target?: Target;
  created_at: string;
  updated_at: string;
  closed_at?: string;
}

export interface PositionGroup {
  id: string;
  exchange: string;
  tradingsymbol: string;
  product: string;
  side: Side | '';
  quantity: number;
  local_quantity?: number;
  broker_quantity?: number;
  average_entry_price?: number;
  trade_ids: string[];
  trade_status: string;
  creation_source: string;
  management_status: ManagementStatus | string;
  converted_from_product?: string;
  converted_to_product?: string;
  warnings?: string[];
  created_at: string;
  updated_at: string;
}

export interface KiteOrder {
  order_id: string;
  exchange: string;
  tradingsymbol: string;
  transaction_type: Side | string;
  quantity: number;
  filled_quantity?: number;
  pending_quantity?: number;
  product: string;
  order_type: string;
  status: OrderStatus | string;
  price?: number;
  trigger_price?: number;
  average_price?: number;
  tag?: string;
  creation_source: string;
  synced_at: string;
}

export interface KitePosition {
  exchange: string;
  tradingsymbol: string;
  product: string;
  quantity: number;
  synced_at: string;
}

export interface DashboardSummary {
  risk_status: RiskStatus;
  active_groups: number;
  managed_groups: number;
  unmanaged_groups: number;
  conflict_groups: number;
  warning_groups: number;
  open_trades: number;
  closed_trades: number;
  open_orders: number;
  rejected_orders: number;
  synced_orders: number;
  synced_positions: number;
  warnings?: Record<string, number>;
  conflicts?: PositionGroup[];
  unmanaged_positions?: PositionGroup[];
  partially_managed?: PositionGroup[];
  recent_open_orders?: KiteOrder[];
  recent_rejected_orders?: KiteOrder[];
}

export interface Metadata {
  runtime: {
    broker: string;
    http_addr: string;
    trade_store_path: string;
    poll_seconds: number;
    sync_poll_seconds: number;
    log_level: string;
    default_product: string;
    default_quantity: number;
    default_market_protection?: number;
    default_stop_loss_points: number;
    default_target_points: number;
    default_sl_limit_offset: number;
    kite_api_key_configured: boolean;
    kite_access_configured: boolean;
    deferred_items?: string[];
  };
  enums: {
    sides: string[];
    products: string[];
    order_types: string[];
    order_statuses: string[];
    trade_statuses: string[];
    creation_sources: string[];
    management_statuses: string[];
    exit_reasons: string[];
    warnings: string[];
    external_exit_roles: string[];
    risk_statuses: string[];
  };
  capabilities: Record<string, boolean>;
  endpoints: Record<string, string[]>;
}

export interface CreateTradeRequest {
  exchange: string;
  tradingsymbol: string;
  side: Side;
  quantity: number;
  product: string;
  order_type: string;
  price?: number;
  market_protection?: number;
  protection?: {
    reference_price?: number;
    stop_loss_points?: number;
    target_points?: number;
    trail_by?: number;
    sl_limit_offset?: number;
  };
}

export interface StopLossRequest {
  trigger_price: number;
  limit_price: number;
  trail_by?: number;
}

export interface TargetRequest {
  price: number;
}

export interface ApiErrorBody {
  kind: string;
  code: string;
  message: string;
}
