import type {
  ApiErrorBody,
  BacktestRequest,
  BacktestResult,
  BacktestSummary,
  Candle,
  CreateTradeRequest,
  DashboardSummary,
  FutureContractsResponse,
  HistoricalSyncRequest,
  HistoricalSyncResult,
  HistoricalInstrument,
  HistoricalUnderlying,
  InstrumentCacheStatus,
  InstrumentSyncResult,
  KiteOrder,
  KitePosition,
  LTPResponse,
  ManagedTrade,
  Metadata,
  OptionContractsResponse,
  OptionQuery,
  PaperRunRequest,
  PaperRunResult,
  PaperRunSummary,
  PositionGroup,
  StopLossRequest,
  TargetRequest,
} from './types';

const API_BASE = import.meta.env.VITE_API_BASE ?? '/api';

export class ApiError extends Error {
  status: number;
  body?: ApiErrorBody;

  constructor(status: number, message: string, body?: ApiErrorBody) {
    super(message);
    this.status = status;
    this.body = body;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers ?? {}),
    },
    ...init,
  });

  const text = await response.text();
  const data = text ? JSON.parse(text) : null;
  if (!response.ok) {
    const body = data as ApiErrorBody | undefined;
    throw new ApiError(response.status, body?.message ?? response.statusText, body);
  }
  return data as T;
}

function post<T>(path: string, body?: unknown): Promise<T> {
  return request<T>(path, {
    method: 'POST',
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

function del<T>(path: string, body?: unknown): Promise<T> {
  return request<T>(path, {
    method: 'DELETE',
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

function queryString(params: object) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') query.set(key, String(value));
  }
  const raw = query.toString();
  return raw ? `?${raw}` : '';
}

export const api = {
  metadata: () => request<Metadata>('/metadata'),
  dashboard: () => request<DashboardSummary>('/dashboard'),
  sync: () => post<unknown>('/sync/kite'),
  trades: () => request<ManagedTrade[]>('/trades'),
  groups: () => request<PositionGroup[]>('/groups'),
  conflicts: () => request<PositionGroup[]>('/conflicts'),
  orders: () => request<KiteOrder[]>('/orders'),
  positions: () => request<KitePosition[]>('/positions'),
  livePositions: () => request<KitePosition[]>('/positions/live'),
  syncInstruments: (exchange = 'NFO') => post<InstrumentSyncResult>(`/instruments/sync${queryString({ exchange })}`),
  instrumentCacheStatus: (exchanges = 'NFO,BFO,MCX,NSE,BSE') => request<InstrumentCacheStatus[]>(`/instruments/cache-status${queryString({ exchanges })}`),
  instrumentUnderlyings: (exchange = 'NFO') => request<string[]>(`/instruments/underlyings${queryString({ exchange })}`),
  instrumentExpiries: (exchange: string, underlying: string) => request<string[]>(`/instruments/expiries${queryString({ exchange, underlying })}`),
  futureUnderlyings: (exchange = 'MCX') => request<string[]>(`/instruments/future-underlyings${queryString({ exchange })}`),
  futureContracts: (exchange: string, underlying: string, product?: string) => request<FutureContractsResponse>(`/instruments/futures${queryString({ exchange, underlying, product })}`),
  optionContracts: (query: OptionQuery) => request<OptionContractsResponse>(`/instruments/options${queryString(query)}`),
  syncHistorical: (body: HistoricalSyncRequest) => post<HistoricalSyncResult>('/historical/sync', body),
  historicalInstruments: (query: { exchange?: string; underlying?: string; expiry?: string; instrument_type?: string; tradingsymbol?: string; limit?: number }) =>
    request<HistoricalInstrument[]>(`/historical/instruments${queryString(query)}`),
  historicalUnderlyings: (exchange: string, q?: string, limit = 50) =>
    request<HistoricalUnderlying[]>(`/historical/underlyings${queryString({ exchange, q, limit })}`),
  historicalCandles: (query: { exchange: string; tradingsymbol: string; interval: string; from: string; to: string }) =>
    request<Candle[]>(`/historical/candles${queryString(query)}`),
  runBacktest: (body: BacktestRequest) => post<BacktestResult>('/backtests', body),
  backtests: () => request<BacktestSummary[]>('/backtests'),
  backtest: (id: string) => request<BacktestResult>(`/backtests/${encodeURIComponent(id)}`),
  runPaper: (body: PaperRunRequest) => post<PaperRunResult>('/paper-runs', body),
  paperRuns: () => request<PaperRunSummary[]>('/paper-runs'),
  paperRun: (id: string) => request<PaperRunResult>(`/paper-runs/${encodeURIComponent(id)}`),
  ltp: (exchange: string, symbol: string) => request<LTPResponse>(`/market/ltp${queryString({ exchange, symbol })}`),
  marketGroups: () => request<PositionGroup[]>('/market/groups'),
  createTrade: (body: CreateTradeRequest) => post<ManagedTrade>('/trades', body),
  addStopLoss: (tradeID: string, body: StopLossRequest) => post<ManagedTrade>(`/trades/${encodeURIComponent(tradeID)}/stop-loss`, body),
  removeStopLoss: (tradeID: string) => del<ManagedTrade>(`/trades/${encodeURIComponent(tradeID)}/stop-loss`),
  addTarget: (tradeID: string, body: TargetRequest) => post<ManagedTrade>(`/trades/${encodeURIComponent(tradeID)}/target`, body),
  removeTarget: (tradeID: string) => del<ManagedTrade>(`/trades/${encodeURIComponent(tradeID)}/target`),
  exitTrade: (tradeID: string) => post<ManagedTrade>(`/trades/${encodeURIComponent(tradeID)}/exit`),
  queueAMOExitTrade: (tradeID: string) => post<ManagedTrade>(`/trades/${encodeURIComponent(tradeID)}/exit/amo`),
  cancelEntry: (tradeID: string) => post<ManagedTrade>(`/trades/${encodeURIComponent(tradeID)}/cancel-entry`),
  applyProductConversion: (tradeID: string) => post<ManagedTrade>(`/trades/${encodeURIComponent(tradeID)}/product-conversion/apply`),
  takeOverGroup: (groupID: string, entryPrice?: number) =>
    post<ManagedTrade>(`/groups/${encodeURIComponent(groupID)}/take-over`, entryPrice ? { entry_price: entryPrice } : undefined),
  addGroupStopLoss: (groupID: string, body: StopLossRequest) =>
    post<ManagedTrade>(`/groups/${encodeURIComponent(groupID)}/stop-loss`, body),
  removeGroupStopLoss: (groupID: string) => del<ManagedTrade>(`/groups/${encodeURIComponent(groupID)}/stop-loss`),
  addGroupTarget: (groupID: string, body: TargetRequest) => post<ManagedTrade>(`/groups/${encodeURIComponent(groupID)}/target`, body),
  removeGroupTarget: (groupID: string) => del<ManagedTrade>(`/groups/${encodeURIComponent(groupID)}/target`),
  exitGroup: (groupID: string) => post<ManagedTrade>(`/groups/${encodeURIComponent(groupID)}/exit`),
  queueAMOExitGroup: (groupID: string) => post<ManagedTrade>(`/groups/${encodeURIComponent(groupID)}/exit/amo`),
  cancelOrder: (orderID: string) => post<unknown>(`/orders/${encodeURIComponent(orderID)}/cancel`),
  linkExternalExit: (groupID: string, orderID: string, role: string) =>
    post<ManagedTrade>(`/groups/${encodeURIComponent(groupID)}/external-exit/link`, { order_id: orderID, role }),
  unlinkExternalExit: (groupID: string, role: string, orderID?: string) =>
    del<ManagedTrade>(`/groups/${encodeURIComponent(groupID)}/external-exit/link`, { order_id: orderID, role }),
};
