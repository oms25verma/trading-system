import { useEffect, useMemo, useState } from 'react';
import {
  Activity,
  AlertTriangle,
  ArrowDownToLine,
  Ban,
  CheckCircle2,
  CircleDollarSign,
  Crosshair,
  ExternalLink,
  LayoutDashboard,
  Link2,
  ListChecks,
  Loader2,
  LogOut,
  Plus,
  RefreshCw,
  Shield,
  Target,
  Trash2,
  XCircle,
} from 'lucide-react';
import { ApiError, api } from './api';
import type {
  CreateTradeRequest,
  DashboardSummary,
  KiteOrder,
  KitePosition,
  ManagedTrade,
  Metadata,
  PositionGroup,
  Side,
  StopLossRequest,
  TargetRequest,
} from './types';

type View = 'dashboard' | 'groups' | 'orders' | 'trades' | 'conflicts';
type SelectOption = string | { value: string; label: string };
type Action =
  | { type: 'create-trade' }
  | { type: 'stop-loss'; trade?: ManagedTrade; group?: PositionGroup }
  | { type: 'target'; trade?: ManagedTrade; group?: PositionGroup }
  | { type: 'take-over'; group: PositionGroup }
  | { type: 'link-exit'; group: PositionGroup };

interface Snapshot {
  metadata?: Metadata;
  dashboard?: DashboardSummary;
  groups: PositionGroup[];
  conflicts: PositionGroup[];
  orders: KiteOrder[];
  trades: ManagedTrade[];
  positions: KitePosition[];
}

const emptySnapshot: Snapshot = {
  groups: [],
  conflicts: [],
  orders: [],
  trades: [],
  positions: [],
};

export function App() {
  const [view, setView] = useState<View>('dashboard');
  const [snapshot, setSnapshot] = useState<Snapshot>(emptySnapshot);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState('');
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const [action, setAction] = useState<Action | null>(null);

  async function load(options?: { silent?: boolean }) {
    if (!options?.silent) setLoading(true);
    setError('');
    try {
      const [metadata, dashboard, groups, conflicts, orders, trades, positions] = await Promise.all([
        api.metadata(),
        api.dashboard(),
        api.groups(),
        api.conflicts(),
        api.orders(),
        api.trades(),
        api.positions(),
      ]);
      setSnapshot({ metadata, dashboard, groups, conflicts, orders, trades, positions });
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  async function run(label: string, fn: () => Promise<unknown>) {
    setBusy(label);
    setError('');
    setNotice('');
    try {
      await fn();
      setNotice(label);
      setAction(null);
      await load({ silent: true });
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy('');
    }
  }

  const openTrades = useMemo(() => snapshot.trades.filter((trade) => trade.trade_status !== 'CLOSED'), [snapshot.trades]);
  const openOrders = useMemo(() => snapshot.orders.filter((order) => order.status === 'OPEN'), [snapshot.orders]);

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand-block">
          <div className="brand-mark">TS</div>
          <div>
            <h1>Trading System</h1>
            <p>{snapshot.metadata?.runtime.broker ?? 'broker'} mode</p>
          </div>
        </div>
        <nav className="nav-list" aria-label="Primary">
          <NavButton icon={<LayoutDashboard />} label="Dashboard" active={view === 'dashboard'} onClick={() => setView('dashboard')} />
          <NavButton icon={<Activity />} label="Positions" active={view === 'groups'} onClick={() => setView('groups')} count={snapshot.groups.length} />
          <NavButton icon={<ListChecks />} label="Orders" active={view === 'orders'} onClick={() => setView('orders')} count={openOrders.length} />
          <NavButton icon={<CircleDollarSign />} label="Trades" active={view === 'trades'} onClick={() => setView('trades')} count={openTrades.length} />
          <NavButton icon={<AlertTriangle />} label="Conflicts" active={view === 'conflicts'} onClick={() => setView('conflicts')} count={snapshot.conflicts.length} />
        </nav>
        <div className="sidebar-footer">
          <button className="icon-text-button" onClick={() => void run('Synced Kite snapshots', api.sync)} disabled={!!busy}>
            {busy === 'Synced Kite snapshots' ? <Loader2 className="spin" /> : <RefreshCw />}
            Sync Kite
          </button>
          <button className="icon-text-button primary" onClick={() => setAction({ type: 'create-trade' })}>
            <Plus />
            New Trade
          </button>
        </div>
      </aside>

      <main className="main-surface">
        <header className="topbar">
          <div>
            <p className="eyebrow">Live operations</p>
            <h2>{titleFor(view)}</h2>
          </div>
          <div className="status-stack">
            <StatusPill value={snapshot.dashboard?.risk_status ?? 'OK'} />
            <button className="icon-button" onClick={() => void load()} title="Refresh" aria-label="Refresh">
              {loading ? <Loader2 className="spin" /> : <RefreshCw />}
            </button>
          </div>
        </header>

        {error && <Toast tone="danger" message={error} onClose={() => setError('')} />}
        {notice && <Toast tone="success" message={notice} onClose={() => setNotice('')} />}

        {loading ? (
          <LoadingState />
        ) : (
          <>
            {view === 'dashboard' && (
              <DashboardView
                snapshot={snapshot}
                onCreate={() => setAction({ type: 'create-trade' })}
                onViewConflicts={() => setView('conflicts')}
              />
            )}
            {view === 'groups' && (
              <GroupsView
                groups={snapshot.groups}
                onStopLoss={(group) => setAction({ type: 'stop-loss', group })}
                onTarget={(group) => setAction({ type: 'target', group })}
                onExit={(group) => void run('Exited group', () => api.exitGroup(group.id))}
                onTakeOver={(group) => setAction({ type: 'take-over', group })}
              />
            )}
            {view === 'orders' && (
              <OrdersView orders={snapshot.orders} onCancel={(order) => void run('Cancelled order', () => api.cancelOrder(order.order_id))} />
            )}
            {view === 'trades' && (
              <TradesView
                trades={snapshot.trades}
                onStopLoss={(trade) => setAction({ type: 'stop-loss', trade })}
                onTarget={(trade) => setAction({ type: 'target', trade })}
                onExit={(trade) => void run('Exited trade', () => api.exitTrade(trade.id))}
                onCancelEntry={(trade) => void run('Cancelled entry', () => api.cancelEntry(trade.id))}
              />
            )}
            {view === 'conflicts' && (
              <ConflictsView
                groups={snapshot.conflicts}
                orders={snapshot.orders}
                onLink={(group) => setAction({ type: 'link-exit', group })}
                onTakeOver={(group) => setAction({ type: 'take-over', group })}
              />
            )}
          </>
        )}
      </main>

      {action && (
        <ActionPanel
          action={action}
          metadata={snapshot.metadata}
          orders={snapshot.orders}
          onClose={() => setAction(null)}
          onRun={run}
        />
      )}
    </div>
  );
}

function NavButton(props: { icon: React.ReactNode; label: string; active: boolean; count?: number; onClick: () => void }) {
  return (
    <button className={`nav-button ${props.active ? 'active' : ''}`} onClick={props.onClick}>
      <span className="nav-icon">{props.icon}</span>
      <span>{props.label}</span>
      {props.count !== undefined && <span className="nav-count">{props.count}</span>}
    </button>
  );
}

function DashboardView(props: { snapshot: Snapshot; onCreate: () => void; onViewConflicts: () => void }) {
  const dashboard = props.snapshot.dashboard;
  return (
    <div className="view-stack">
      <section className="metric-grid">
        <Metric label="Active groups" value={dashboard?.active_groups ?? 0} icon={<Activity />} />
        <Metric label="Open orders" value={dashboard?.open_orders ?? 0} icon={<ListChecks />} />
        <Metric label="Open trades" value={dashboard?.open_trades ?? 0} icon={<CircleDollarSign />} />
        <Metric label="Conflicts" value={dashboard?.conflict_groups ?? 0} icon={<AlertTriangle />} tone={dashboard?.conflict_groups ? 'danger' : 'normal'} />
      </section>

      <section className="work-grid">
        <div className="panel">
          <PanelTitle icon={<Shield />} title="Risk Summary" action={<button className="text-button" onClick={props.onViewConflicts}>Review</button>} />
          <div className="risk-body">
            <StatusPill value={dashboard?.risk_status ?? 'OK'} />
            <div className="risk-counts">
              <span>{dashboard?.managed_groups ?? 0} managed</span>
              <span>{dashboard?.unmanaged_groups ?? 0} unmanaged</span>
              <span>{dashboard?.warning_groups ?? 0} warnings</span>
            </div>
          </div>
          <WarningList warnings={dashboard?.warnings} />
        </div>
        <div className="panel">
          <PanelTitle icon={<ListChecks />} title="Recent Open Orders" />
          <MiniOrderList orders={dashboard?.recent_open_orders ?? []} />
        </div>
      </section>

      <section className="table-section">
        <PanelTitle icon={<Activity />} title="Active Position Groups" action={<button className="text-button" onClick={props.onCreate}>New trade</button>} />
        <GroupTable groups={props.snapshot.groups.slice(0, 8)} compact />
      </section>
    </div>
  );
}

function GroupsView(props: {
  groups: PositionGroup[];
  onStopLoss: (group: PositionGroup) => void;
  onTarget: (group: PositionGroup) => void;
  onExit: (group: PositionGroup) => void;
  onTakeOver: (group: PositionGroup) => void;
}) {
  return (
    <section className="table-section">
      <PanelTitle icon={<Activity />} title="Position Groups" />
      <GroupTable groups={props.groups} actions={props} />
    </section>
  );
}

function OrdersView(props: { orders: KiteOrder[]; onCancel: (order: KiteOrder) => void }) {
  const orders = useMemo(() => [...props.orders].sort((left, right) => orderMillis(right) - orderMillis(left)), [props.orders]);

  return (
    <section className="table-section">
      <PanelTitle icon={<ListChecks />} title="Orderbook" />
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Time</th>
              <th>Order</th>
              <th>Symbol</th>
              <th>Side</th>
              <th>Qty</th>
              <th>Type</th>
              <th>Trigger</th>
              <th>Order Price</th>
              <th>Avg Price</th>
              <th>Status</th>
              <th>Source</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {orders.map((order) => (
              <tr key={order.order_id}>
                <td>{formatOrderTime(order)}</td>
                <td className="mono">{order.order_id}</td>
                <td>{order.exchange}:{order.tradingsymbol}</td>
                <td><SidePill side={order.transaction_type} /></td>
                <td>{order.quantity}</td>
                <td>{order.order_type}</td>
                <td>{money(order.trigger_price)}</td>
                <td>{money(order.price)}</td>
                <td>{money(order.average_price)}</td>
                <td><OrderStatusPill status={order.status} /></td>
                <td>{order.creation_source}</td>
                <td className="row-actions">
                  {order.status === 'OPEN' && (
                    <button className="icon-button danger" onClick={() => props.onCancel(order)} title="Cancel order" aria-label="Cancel order">
                      <Ban />
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {props.orders.length === 0 && <EmptyState label="No synced orders" />}
    </section>
  );
}

function TradesView(props: {
  trades: ManagedTrade[];
  onStopLoss: (trade: ManagedTrade) => void;
  onTarget: (trade: ManagedTrade) => void;
  onExit: (trade: ManagedTrade) => void;
  onCancelEntry: (trade: ManagedTrade) => void;
}) {
  return (
    <section className="table-section">
      <PanelTitle icon={<CircleDollarSign />} title="Managed Trades" />
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Trade</th>
              <th>Symbol</th>
              <th>Side</th>
              <th>Qty</th>
              <th>Entry</th>
              <th>SL</th>
              <th>Target</th>
              <th>Status</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {props.trades.map((trade) => (
              <tr key={trade.id}>
                <td className="mono">{trade.id}</td>
                <td>{trade.exchange}:{trade.tradingsymbol}</td>
                <td><SidePill side={trade.side} /></td>
                <td>{trade.quantity}</td>
                <td>{money(trade.entry_price)} <span className="muted">{trade.entry_status}</span></td>
                <td>{trade.stop_loss ? `${money(trade.stop_loss.trigger_price)} / ${money(trade.stop_loss.limit_price)}` : dash(trade.pending_stop_loss ? 'pending' : '')}</td>
                <td>{trade.target ? money(trade.target.price) : dash(trade.pending_target ? 'pending' : '')}</td>
                <td><TradeStatusPill status={trade.trade_status ?? 'OPEN'} /></td>
                <td className="row-actions">
                  {trade.trade_status !== 'CLOSED' && (
                    <>
                      <button className="icon-button" onClick={() => props.onStopLoss(trade)} title="Set stop-loss" aria-label="Set stop-loss"><Shield /></button>
                      <button className="icon-button" onClick={() => props.onTarget(trade)} title="Set target" aria-label="Set target"><Target /></button>
                      {trade.entry_status !== 'COMPLETE' && <button className="icon-button danger" onClick={() => props.onCancelEntry(trade)} title="Cancel entry" aria-label="Cancel entry"><Trash2 /></button>}
                      <button className="icon-button danger" onClick={() => props.onExit(trade)} title="Exit trade" aria-label="Exit trade"><LogOut /></button>
                    </>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {props.trades.length === 0 && <EmptyState label="No managed trades" />}
    </section>
  );
}

function ConflictsView(props: {
  groups: PositionGroup[];
  orders: KiteOrder[];
  onLink: (group: PositionGroup) => void;
  onTakeOver: (group: PositionGroup) => void;
}) {
  return (
    <section className="table-section">
      <PanelTitle icon={<AlertTriangle />} title="Needs Attention" />
      <GroupTable
        groups={props.groups}
        actions={{
          onStopLoss: props.onLink,
          onTarget: props.onLink,
          onExit: props.onLink,
          onTakeOver: props.onTakeOver,
        }}
        conflictMode
      />
      {props.groups.length === 0 && <EmptyState label="No conflicts or warnings" />}
    </section>
  );
}

function GroupTable(props: {
  groups: PositionGroup[];
  compact?: boolean;
  conflictMode?: boolean;
  actions?: {
    onStopLoss: (group: PositionGroup) => void;
    onTarget: (group: PositionGroup) => void;
    onExit: (group: PositionGroup) => void;
    onTakeOver: (group: PositionGroup) => void;
  };
}) {
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Group</th>
            <th>Side</th>
            <th>Qty</th>
            <th>Avg Entry</th>
            <th>Local/Broker</th>
            <th>Status</th>
            {!props.compact && <th>Warnings</th>}
            {!props.compact && <th></th>}
          </tr>
        </thead>
        <tbody>
          {props.groups.map((group) => (
            <tr key={group.id}>
              <td>
                <strong>{group.tradingsymbol}</strong>
                <span className="subline">{group.exchange} · {group.product}</span>
              </td>
              <td>{group.side ? <SidePill side={group.side} /> : '-'}</td>
              <td>{group.quantity}</td>
              <td>{money(group.average_entry_price)}</td>
              <td>{group.local_quantity ?? 0}/{group.broker_quantity ?? 0}</td>
              <td><ManagementPill status={group.management_status} /></td>
              {!props.compact && <td><WarningChips warnings={group.warnings ?? []} /></td>}
              {!props.compact && (
                <td className="row-actions">
                  {group.management_status === 'UNMANAGED' ? (
                    <button className="icon-button" onClick={() => props.actions?.onTakeOver(group)} title="Take over" aria-label="Take over"><ArrowDownToLine /></button>
                  ) : props.conflictMode ? (
                    <button className="icon-button" onClick={() => props.actions?.onStopLoss(group)} title="Link external order" aria-label="Link external order"><Link2 /></button>
                  ) : (
                    <>
                      <button className="icon-button" onClick={() => props.actions?.onStopLoss(group)} title="Set stop-loss" aria-label="Set stop-loss"><Shield /></button>
                      <button className="icon-button" onClick={() => props.actions?.onTarget(group)} title="Set target" aria-label="Set target"><Target /></button>
                      <button className="icon-button danger" onClick={() => props.actions?.onExit(group)} title="Exit group" aria-label="Exit group"><LogOut /></button>
                    </>
                  )}
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ActionPanel(props: {
  action: Action;
  metadata?: Metadata;
  orders: KiteOrder[];
  onClose: () => void;
  onRun: (label: string, fn: () => Promise<unknown>) => Promise<void>;
}) {
  const title =
    props.action.type === 'create-trade' ? 'New Trade' :
    props.action.type === 'stop-loss' ? 'Set Stop-Loss' :
    props.action.type === 'target' ? 'Set Target' :
    props.action.type === 'take-over' ? 'Take Over Position' :
    'Link External Exit';

  return (
    <div className="drawer-backdrop" role="presentation">
      <aside className="drawer" aria-label={title}>
        <div className="drawer-head">
          <h3>{title}</h3>
          <button className="icon-button" onClick={props.onClose} title="Close" aria-label="Close"><XCircle /></button>
        </div>
        {props.action.type === 'create-trade' && <CreateTradeForm metadata={props.metadata} onRun={props.onRun} />}
        {props.action.type === 'stop-loss' && <StopLossForm action={props.action} onRun={props.onRun} />}
        {props.action.type === 'target' && <TargetForm action={props.action} onRun={props.onRun} />}
        {props.action.type === 'take-over' && <TakeOverForm group={props.action.group} onRun={props.onRun} />}
        {props.action.type === 'link-exit' && <LinkExitForm group={props.action.group} orders={props.orders} onRun={props.onRun} />}
      </aside>
    </div>
  );
}

function CreateTradeForm(props: { metadata?: Metadata; onRun: (label: string, fn: () => Promise<unknown>) => Promise<void> }) {
  const defaults = props.metadata?.runtime;
  const watchlist = defaults?.symbol_watchlist ?? [];
  const firstSymbol = watchlist[0];
  const [selectedSymbol, setSelectedSymbol] = useState(firstSymbol ? symbolKey(firstSymbol) : '');
  const [form, setForm] = useState<CreateTradeRequest>({
    exchange: firstSymbol?.exchange ?? 'NSE',
    tradingsymbol: firstSymbol?.tradingsymbol ?? '',
    side: 'BUY',
    quantity: firstSymbol?.default_quantity || defaults?.default_quantity || 1,
    product: firstSymbol?.product ?? defaults?.default_product ?? 'MIS',
    order_type: 'MARKET',
    market_protection: defaults?.default_market_protection,
    protection: {
      stop_loss_points: defaults?.default_stop_loss_points || undefined,
      target_points: defaults?.default_target_points || undefined,
      sl_limit_offset: defaults?.default_sl_limit_offset || undefined,
    },
  });
  const [withProtection, setWithProtection] = useState(true);

  function submit() {
    const body = { ...form, tradingsymbol: form.tradingsymbol.trim().toUpperCase() };
    if (!withProtection) delete body.protection;
    if (body.order_type === 'MARKET') delete body.price;
    return props.onRun('Created trade', () => api.createTrade(body));
  }

  function selectSymbol(value: string) {
    setSelectedSymbol(value);
    const item = watchlist.find((symbol) => symbolKey(symbol) === value);
    if (!item) return;
    setForm({
      ...form,
      exchange: item.exchange,
      tradingsymbol: item.tradingsymbol,
      product: item.product,
      quantity: item.default_quantity || form.quantity,
    });
  }

  return (
    <FormShell onSubmit={submit}>
      {watchlist.length > 0 ? (
        <Select
          label="Symbol"
          value={selectedSymbol}
          options={watchlist.map((symbol) => ({ value: symbolKey(symbol), label: symbolLabel(symbol) }))}
          onChange={selectSymbol}
        />
      ) : (
        <>
          <Input label="Exchange" value={form.exchange} onChange={(exchange) => setForm({ ...form, exchange: exchange.toUpperCase() })} />
          <Input label="Symbol" value={form.tradingsymbol} onChange={(tradingsymbol) => setForm({ ...form, tradingsymbol })} />
        </>
      )}
      <Segmented label="Side" value={form.side} options={['BUY', 'SELL']} onChange={(side) => setForm({ ...form, side: side as Side })} />
      <Input label="Quantity" type="number" value={form.quantity} onChange={(quantity) => setForm({ ...form, quantity: Number(quantity) })} />
      {watchlist.length > 0 ? (
        <div className="selected-box">
          <strong>{form.exchange}:{form.tradingsymbol}</strong>
          <span>{form.product}</span>
        </div>
      ) : (
        <Select label="Product" value={form.product} options={props.metadata?.enums.products ?? ['MIS', 'NRML']} onChange={(product) => setForm({ ...form, product })} />
      )}
      <Select label="Order Type" value={form.order_type} options={['MARKET', 'LIMIT']} onChange={(order_type) => setForm({ ...form, order_type })} />
      {form.order_type === 'LIMIT' && <Input label="Limit Price" type="number" value={form.price ?? ''} onChange={(price) => setForm({ ...form, price: Number(price) })} />}
      <label className="check-row">
        <input type="checkbox" checked={withProtection} onChange={(event) => setWithProtection(event.target.checked)} />
        <span>Protection</span>
      </label>
      {withProtection && (
        <div className="form-grid two">
          <Input label="Reference Price" type="number" value={form.protection?.reference_price ?? ''} onChange={(value) => setForm({ ...form, protection: { ...form.protection, reference_price: optionalNumber(value) } })} />
          <Input label="SL Points" type="number" value={form.protection?.stop_loss_points ?? ''} onChange={(value) => setForm({ ...form, protection: { ...form.protection, stop_loss_points: optionalNumber(value) } })} />
          <Input label="Target Points" type="number" value={form.protection?.target_points ?? ''} onChange={(value) => setForm({ ...form, protection: { ...form.protection, target_points: optionalNumber(value) } })} />
          <Input label="SL Offset" type="number" value={form.protection?.sl_limit_offset ?? ''} onChange={(value) => setForm({ ...form, protection: { ...form.protection, sl_limit_offset: optionalNumber(value) } })} />
          <Input label="Trail By" type="number" value={form.protection?.trail_by ?? ''} onChange={(value) => setForm({ ...form, protection: { ...form.protection, trail_by: optionalNumber(value) } })} />
        </div>
      )}
      <button className="icon-text-button primary form-submit" type="submit"><Plus /> Create</button>
    </FormShell>
  );
}

function StopLossForm(props: { action: Extract<Action, { type: 'stop-loss' }>; onRun: (label: string, fn: () => Promise<unknown>) => Promise<void> }) {
  const [form, setForm] = useState<StopLossRequest>({ trigger_price: 0, limit_price: 0 });
  const target = props.action.trade ?? props.action.group;
  function submit() {
    if (!target) return;
    if ('id' in target && 'trade_ids' in target) {
      return props.onRun('Set group stop-loss', () => api.addGroupStopLoss(target.id, form));
    }
    return props.onRun('Set trade stop-loss', () => api.addStopLoss((target as ManagedTrade).id, form));
  }
  return (
    <FormShell onSubmit={submit}>
      <Input label="Trigger Price" type="number" value={form.trigger_price || ''} onChange={(value) => setForm({ ...form, trigger_price: Number(value) })} />
      <Input label="Limit Price" type="number" value={form.limit_price || ''} onChange={(value) => setForm({ ...form, limit_price: Number(value) })} />
      <Input label="Trail By" type="number" value={form.trail_by ?? ''} onChange={(value) => setForm({ ...form, trail_by: Number(value) })} />
      <button className="icon-text-button primary form-submit" type="submit"><Shield /> Save SL</button>
    </FormShell>
  );
}

function TargetForm(props: { action: Extract<Action, { type: 'target' }>; onRun: (label: string, fn: () => Promise<unknown>) => Promise<void> }) {
  const [form, setForm] = useState<TargetRequest>({ price: 0 });
  const target = props.action.trade ?? props.action.group;
  function submit() {
    if (!target) return;
    if ('id' in target && 'trade_ids' in target) {
      return props.onRun('Set group target', () => api.addGroupTarget(target.id, form));
    }
    return props.onRun('Set trade target', () => api.addTarget((target as ManagedTrade).id, form));
  }
  return (
    <FormShell onSubmit={submit}>
      <Input label="Target Price" type="number" value={form.price || ''} onChange={(value) => setForm({ price: Number(value) })} />
      <button className="icon-text-button primary form-submit" type="submit"><Target /> Save Target</button>
    </FormShell>
  );
}

function TakeOverForm(props: { group: PositionGroup; onRun: (label: string, fn: () => Promise<unknown>) => Promise<void> }) {
  const [entryPrice, setEntryPrice] = useState('');
  return (
    <FormShell onSubmit={() => props.onRun('Took over position', () => api.takeOverGroup(props.group.id, entryPrice ? Number(entryPrice) : undefined))}>
      <div className="selected-box">
        <strong>{props.group.id}</strong>
        <span>{props.group.side} {props.group.quantity}</span>
      </div>
      <Input label="Entry Price" type="number" value={entryPrice} onChange={setEntryPrice} />
      <button className="icon-text-button primary form-submit" type="submit"><ArrowDownToLine /> Take Over</button>
    </FormShell>
  );
}

function LinkExitForm(props: { group: PositionGroup; orders: KiteOrder[]; onRun: (label: string, fn: () => Promise<unknown>) => Promise<void> }) {
  const matchingOrders = props.orders.filter((order) =>
    order.status === 'OPEN' &&
    order.exchange === props.group.exchange &&
    order.tradingsymbol === props.group.tradingsymbol &&
    order.product === props.group.product,
  );
  const [orderID, setOrderID] = useState(matchingOrders[0]?.order_id ?? '');
  const [role, setRole] = useState('stop_loss');
  return (
    <FormShell onSubmit={() => props.onRun('Linked external exit', () => api.linkExternalExit(props.group.id, orderID, role))}>
      <Select label="Order" value={orderID} options={matchingOrders.map((order) => order.order_id)} onChange={setOrderID} />
      <Segmented label="Role" value={role} options={['stop_loss', 'target']} onChange={setRole} />
      <button className="icon-text-button primary form-submit" type="submit"><Link2 /> Link Order</button>
    </FormShell>
  );
}

function FormShell(props: { children: React.ReactNode; onSubmit: () => void | Promise<void> }) {
  return (
    <form className="form-stack" onSubmit={(event) => { event.preventDefault(); void props.onSubmit(); }}>
      {props.children}
    </form>
  );
}

function Input(props: { label: string; value: string | number; type?: string; onChange: (value: string) => void }) {
  return (
    <label className="field">
      <span>{props.label}</span>
      <input type={props.type ?? 'text'} value={props.value} onChange={(event) => props.onChange(event.target.value)} />
    </label>
  );
}

function Select(props: { label: string; value: string; options: SelectOption[]; onChange: (value: string) => void }) {
  return (
    <label className="field">
      <span>{props.label}</span>
      <select value={props.value} onChange={(event) => props.onChange(event.target.value)}>
        {props.options.map((option) => {
          const value = typeof option === 'string' ? option : option.value;
          const label = typeof option === 'string' ? option : option.label;
          return <option key={value} value={value}>{label}</option>;
        })}
      </select>
    </label>
  );
}

function Segmented(props: { label: string; value: string; options: string[]; onChange: (value: string) => void }) {
  return (
    <div className="field">
      <span>{props.label}</span>
      <div className="segmented">
        {props.options.map((option) => (
          <button key={option} type="button" className={option === props.value ? 'selected' : ''} onClick={() => props.onChange(option)}>
            {option}
          </button>
        ))}
      </div>
    </div>
  );
}

function Metric(props: { label: string; value: number; icon: React.ReactNode; tone?: 'normal' | 'danger' }) {
  return (
    <div className={`metric ${props.tone === 'danger' ? 'danger' : ''}`}>
      <span className="metric-icon">{props.icon}</span>
      <span className="metric-value">{props.value}</span>
      <span className="metric-label">{props.label}</span>
    </div>
  );
}

function PanelTitle(props: { icon: React.ReactNode; title: string; action?: React.ReactNode }) {
  return (
    <div className="panel-title">
      <div><span>{props.icon}</span><h3>{props.title}</h3></div>
      {props.action}
    </div>
  );
}

function StatusPill(props: { value: string }) {
  return <span className={`pill ${props.value.toLowerCase()}`}>{props.value}</span>;
}

function ManagementPill(props: { status: string }) {
  return <span className={`pill management ${props.status.toLowerCase()}`}>{props.status}</span>;
}

function OrderStatusPill(props: { status: string }) {
  return <span className={`pill order ${props.status.toLowerCase()}`}>{props.status}</span>;
}

function TradeStatusPill(props: { status: string }) {
  return <span className={`pill trade ${props.status.toLowerCase()}`}>{props.status}</span>;
}

function SidePill(props: { side: string }) {
  return <span className={`side-pill ${props.side.toLowerCase()}`}>{props.side}</span>;
}

function WarningChips(props: { warnings: string[] }) {
  if (!props.warnings.length) return <span className="muted">-</span>;
  return <div className="chip-row">{props.warnings.map((warning) => <span className="chip" key={warning}>{warning}</span>)}</div>;
}

function WarningList(props: { warnings?: Record<string, number> }) {
  const entries = Object.entries(props.warnings ?? {});
  if (!entries.length) return <p className="muted panel-note">No active warnings</p>;
  return (
    <div className="warning-list">
      {entries.map(([warning, count]) => <span key={warning}><AlertTriangle /> {warning} <strong>{count}</strong></span>)}
    </div>
  );
}

function MiniOrderList(props: { orders: KiteOrder[] }) {
  if (!props.orders.length) return <EmptyState label="No open orders" compact />;
  const orders = [...props.orders].sort((left, right) => orderMillis(right) - orderMillis(left));
  return (
    <div className="mini-list">
      {orders.map((order) => (
        <div className="mini-row" key={order.order_id}>
          <span><strong>{order.tradingsymbol}</strong><small>{formatOrderTime(order)} · {order.order_type} · {order.product}</small></span>
          <SidePill side={order.transaction_type} />
        </div>
      ))}
    </div>
  );
}

function Toast(props: { tone: 'danger' | 'success'; message: string; onClose: () => void }) {
  return (
    <div className={`toast ${props.tone}`}>
      {props.tone === 'success' ? <CheckCircle2 /> : <AlertTriangle />}
      <span>{props.message}</span>
      <button className="icon-button" onClick={props.onClose} title="Dismiss" aria-label="Dismiss"><XCircle /></button>
    </div>
  );
}

function LoadingState() {
  return (
    <div className="loading-state">
      <Loader2 className="spin" />
    </div>
  );
}

function EmptyState(props: { label: string; compact?: boolean }) {
  return (
    <div className={`empty-state ${props.compact ? 'compact' : ''}`}>
      <Crosshair />
      <span>{props.label}</span>
    </div>
  );
}

function titleFor(view: View) {
  switch (view) {
    case 'groups': return 'Position Groups';
    case 'orders': return 'Orderbook';
    case 'trades': return 'Managed Trades';
    case 'conflicts': return 'Needs Attention';
    default: return 'Dashboard';
  }
}

function symbolKey(symbol: { exchange: string; tradingsymbol: string; product: string }) {
  return `${symbol.exchange}:${symbol.tradingsymbol}:${symbol.product}`;
}

function symbolLabel(symbol: { exchange: string; tradingsymbol: string; product: string; name?: string }) {
  const instrument = `${symbol.exchange}:${symbol.tradingsymbol}`;
  return symbol.name ? `${symbol.name} · ${instrument} · ${symbol.product}` : `${instrument} · ${symbol.product}`;
}

function optionalNumber(value: string) {
  return value === '' ? undefined : Number(value);
}

function orderMillis(order: KiteOrder) {
  const value = order.order_timestamp || order.synced_at;
  const millis = Date.parse(value);
  return Number.isNaN(millis) ? 0 : millis;
}

function formatOrderTime(order: KiteOrder) {
  const value = order.order_timestamp || order.synced_at;
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '-';
  return date.toLocaleString('en-IN', {
    day: '2-digit',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

function money(value?: number) {
  if (!value) return '-';
  return value.toLocaleString('en-IN', { maximumFractionDigits: 2 });
}

function dash(value: string) {
  return value || '-';
}

function errorMessage(err: unknown) {
  if (err instanceof ApiError) return `${err.body?.code ?? err.status}: ${err.message}`;
  if (err instanceof Error) return err.message;
  return 'Unexpected error';
}
