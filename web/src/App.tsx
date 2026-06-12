import { useEffect, useMemo, useState } from 'react';
import {
  Activity,
  AlertTriangle,
  ArrowDownToLine,
  Ban,
  CheckCircle2,
  CircleDollarSign,
  Clock3,
  Crosshair,
  Info,
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
  Unlink2,
  XCircle,
} from 'lucide-react';
import { ApiError, api } from './api';
import type {
  CreateTradeRequest,
  DashboardSummary,
  KiteOrder,
  KitePosition,
  LTPResponse,
  ManagedTrade,
  Metadata,
  OptionContract,
  PositionGroup,
  Side,
  StopLossRequest,
  TargetRequest,
} from './types';

type View = 'dashboard' | 'groups' | 'orders' | 'trades' | 'conflicts';
type PositionTab = 'active' | 'closed' | 'unmanaged' | 'conflicts';
type SelectOption = string | { value: string; label: string };
type TableState = { page: number; pageSize: number; status: string; symbol: string };
type PageResult<T> = { items: T[]; page: number; totalPages: number; total: number };
type RiskPreviewData = {
  referencePrice: number;
  stopTrigger: number;
  stopLimit: number;
  targetPrice: number;
  riskAmount: number;
  rewardAmount: number;
  riskReward: number;
};
type Action =
  | { type: 'create-trade' }
  | { type: 'stop-loss'; trade?: ManagedTrade; group?: PositionGroup }
  | { type: 'target'; trade?: ManagedTrade; group?: PositionGroup }
  | { type: 'take-over'; group: PositionGroup }
  | { type: 'link-exit'; group: PositionGroup }
  | { type: 'group-detail'; group: PositionGroup };

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

const MARKET_REFRESH_MS = 5000;
const DEFAULT_OPTION_RANGE_POINTS = 1000;

export function App() {
  const [view, setView] = useState<View>('dashboard');
  const [snapshot, setSnapshot] = useState<Snapshot>(emptySnapshot);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState('');
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const [action, setAction] = useState<Action | null>(null);
  const [lastSyncedAt, setLastSyncedAt] = useState('');
  const [liveRefresh, setLiveRefresh] = useState(true);

  async function load(options?: { silent?: boolean }) {
    if (!options?.silent) setLoading(true);
    setError('');
    try {
      const [metadata, dashboard, groups, conflicts, orders, trades, positions] = await Promise.all([
        api.metadata(),
        api.dashboard(),
        api.marketGroups().catch(() => api.groups()),
        api.conflicts(),
        api.orders(),
        api.trades(),
        api.livePositions().catch(() => api.positions()),
      ]);
      setSnapshot({ metadata, dashboard, groups: mergeLivePositionsIntoGroups(groups, positions), conflicts, orders, trades, positions });
      const latestSync = latestSyncedAt(orders, positions);
      if (latestSync) setLastSyncedAt(latestSync);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  useEffect(() => {
    if (!liveRefresh) return;
    const id = window.setInterval(() => {
      void load({ silent: true });
    }, MARKET_REFRESH_MS);
    return () => window.clearInterval(id);
  }, [liveRefresh]);

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

  function confirmRun(message: string, label: string, fn: () => Promise<unknown>) {
    if (!window.confirm(message)) return;
    void run(label, fn);
  }

  async function syncKite() {
    setBusy('Synced Kite snapshots');
    setError('');
    setNotice('');
    try {
      const result = await api.sync();
      if (isSyncResult(result) && result.synced_at) setLastSyncedAt(result.synced_at);
      setNotice('Synced Kite snapshots');
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
          <button className="icon-text-button" onClick={() => void syncKite()} disabled={!!busy}>
            {busy === 'Synced Kite snapshots' ? <Loader2 className="spin" /> : <RefreshCw />}
            Sync Kite
          </button>
          <span className="sync-time">{lastSyncedAt ? `Last ${formatDateTime(lastSyncedAt)}` : 'Not synced'}</span>
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
            <button className={`icon-text-button compact ${liveRefresh ? 'active' : ''}`} onClick={() => setLiveRefresh(!liveRefresh)}>
              <Activity />
              {liveRefresh ? 'Live' : 'Paused'}
            </button>
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
                trades={snapshot.trades}
                orders={snapshot.orders}
                onStopLoss={(group) => setAction({ type: 'stop-loss', group })}
                onTarget={(group) => setAction({ type: 'target', group })}
                onExit={(group) => confirmRun(`Exit ${group.tradingsymbol} now?`, 'Exited group', () => api.exitGroup(group.id))}
                onAMOExit={(group) => confirmRun(`Queue AMO exit for ${group.tradingsymbol}?`, 'Queued AMO exit', () => api.queueAMOExitGroup(group.id))}
                onRemoveStopLoss={(group) => confirmRun(`Remove stop-loss for ${group.tradingsymbol}?`, 'Removed group stop-loss', () => api.removeGroupStopLoss(group.id))}
                onRemoveTarget={(group) => confirmRun(`Remove target for ${group.tradingsymbol}?`, 'Removed group target', () => api.removeGroupTarget(group.id))}
                onUnlinkStopLoss={(group) => confirmRun(`Unlink stop-loss locally for ${group.tradingsymbol}? This will not cancel the broker order.`, 'Unlinked group stop-loss', () => api.unlinkExternalExit(group.id, 'stop_loss', group.trade_ids.length === 1 ? linkedTrade(snapshot.trades, group)?.stop_order_id : undefined))}
                onUnlinkTarget={(group) => confirmRun(`Unlink target locally for ${group.tradingsymbol}? This will not cancel the broker order.`, 'Unlinked group target', () => api.unlinkExternalExit(group.id, 'target', group.trade_ids.length === 1 ? linkedTrade(snapshot.trades, group)?.target_order_id : undefined))}
                onApplyConversion={(group) => confirmRun(`Apply product conversion for ${group.tradingsymbol}?`, 'Applied product conversion', () => api.applyProductConversion(group.trade_ids[0]))}
                onTakeOver={(group) => setAction({ type: 'take-over', group })}
                onDetails={(group) => setAction({ type: 'group-detail', group })}
              />
            )}
            {view === 'orders' && (
              <OrdersView orders={snapshot.orders} onCancel={(order) => confirmRun(`Cancel order ${order.order_id}?`, 'Cancelled order', () => api.cancelOrder(order.order_id))} />
            )}
            {view === 'trades' && (
              <TradesView
                trades={snapshot.trades}
                onStopLoss={(trade) => setAction({ type: 'stop-loss', trade })}
                onTarget={(trade) => setAction({ type: 'target', trade })}
                onExit={(trade) => confirmRun(`Exit ${trade.tradingsymbol} now?`, 'Exited trade', () => api.exitTrade(trade.id))}
                onAMOExit={(trade) => confirmRun(`Queue AMO exit for ${trade.tradingsymbol}?`, 'Queued AMO exit', () => api.queueAMOExitTrade(trade.id))}
                onRemoveStopLoss={(trade) => confirmRun(`Remove stop-loss for ${trade.tradingsymbol}?`, 'Removed stop-loss', () => api.removeStopLoss(trade.id))}
                onRemoveTarget={(trade) => confirmRun(`Remove target for ${trade.tradingsymbol}?`, 'Removed target', () => api.removeTarget(trade.id))}
                onCancelEntry={(trade) => confirmRun(`Cancel entry for ${trade.tradingsymbol}?`, 'Cancelled entry', () => api.cancelEntry(trade.id))}
              />
            )}
            {view === 'conflicts' && (
              <ConflictsView
                groups={snapshot.conflicts}
                orders={snapshot.orders}
                onLink={(group) => setAction({ type: 'link-exit', group })}
                onTakeOver={(group) => setAction({ type: 'take-over', group })}
                onDetails={(group) => setAction({ type: 'group-detail', group })}
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
          trades={snapshot.trades}
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
  trades: ManagedTrade[];
  orders: KiteOrder[];
  onStopLoss: (group: PositionGroup) => void;
  onTarget: (group: PositionGroup) => void;
  onExit: (group: PositionGroup) => void;
  onAMOExit: (group: PositionGroup) => void;
  onRemoveStopLoss: (group: PositionGroup) => void;
  onRemoveTarget: (group: PositionGroup) => void;
  onUnlinkStopLoss: (group: PositionGroup) => void;
  onUnlinkTarget: (group: PositionGroup) => void;
  onApplyConversion: (group: PositionGroup) => void;
  onTakeOver: (group: PositionGroup) => void;
  onDetails: (group: PositionGroup) => void;
}) {
  const [table, setTable] = useState<TableState>({ page: 1, pageSize: 25, status: '', symbol: '' });
  const [tab, setTab] = useState<PositionTab>('active');
  const closedTrades = useMemo(() => props.trades.filter((trade) => trade.trade_status === 'CLOSED'), [props.trades]);
  const visibleGroups = useMemo(() => props.groups.filter((group) => {
    if (tab === 'active') return group.management_status !== 'CONFLICT';
    if (tab === 'unmanaged') return group.management_status === 'UNMANAGED';
    if (tab === 'conflicts') return groupNeedsAttentionUI(group);
    return false;
  }).filter((group) => matchesTableFilter(group, table)), [props.groups, table, tab]);
  const visibleClosedTrades = useMemo(() => closedTrades.filter((trade) => matchesTableFilter(trade, table)), [closedTrades, table]);
  const groupPage = pageItems(visibleGroups, table);
  const closedPage = pageItems(visibleClosedTrades, table);
  const openPnL = props.groups.reduce((sum, group) => sum + (group.unrealized_pnl ?? 0), 0);
  const closedPnL = closedTrades.reduce((sum, trade) => sum + (closedTradePnL(trade, props.orders) ?? 0), 0);

  return (
    <section className="table-section">
      <PanelTitle icon={<Activity />} title="Position Groups" />
      <div className="pnl-strip">
        <PnLTile label="Open P&L" value={openPnL} />
        <PnLTile label="Closed P&L" value={closedPnL} />
        <PnLTile label="Net P&L" value={openPnL + closedPnL} />
      </div>
      <div className="tab-row">
        <TabButton label="Active" active={tab === 'active'} count={props.groups.filter((group) => group.management_status !== 'CONFLICT').length} onClick={() => setTab('active')} />
        <TabButton label="Closed" active={tab === 'closed'} count={closedTrades.length} onClick={() => setTab('closed')} />
        <TabButton label="Unmanaged" active={tab === 'unmanaged'} count={props.groups.filter((group) => group.management_status === 'UNMANAGED').length} onClick={() => setTab('unmanaged')} />
        <TabButton label="Conflicts" active={tab === 'conflicts'} count={props.groups.filter(groupNeedsAttentionUI).length} onClick={() => setTab('conflicts')} />
      </div>
      <TableControls state={table} total={tab === 'closed' ? closedPage.total : groupPage.total} onChange={setTable} statusOptions={tab === 'closed' ? ['', 'OPEN', 'CLOSED'] : ['', 'MANAGED', 'UNMANAGED', 'PARTIALLY_MANAGED', 'CONFLICT']} />
      {tab === 'closed' ? (
        <>
          <ClosedTradeTable trades={closedPage.items} orders={props.orders} />
          <Pager page={closedPage} state={table} onChange={setTable} />
          {closedPage.total === 0 && <EmptyState label="No closed trades" />}
        </>
      ) : (
        <>
          <GroupTable groups={groupPage.items} actions={props} />
          <Pager page={groupPage} state={table} onChange={setTable} />
          {groupPage.total === 0 && <EmptyState label="No positions" />}
        </>
      )}
    </section>
  );
}

function OrdersView(props: { orders: KiteOrder[]; onCancel: (order: KiteOrder) => void }) {
  const [table, setTable] = useState<TableState>({ page: 1, pageSize: 25, status: '', symbol: '' });
  const orders = useMemo(() => [...props.orders]
    .filter((order) => matchesTableFilter(order, table))
    .sort((left, right) => orderMillis(right) - orderMillis(left)), [props.orders, table]);
  const page = pageItems(orders, table);

  return (
    <section className="table-section">
      <PanelTitle icon={<ListChecks />} title="Orderbook" />
      <TableControls state={table} total={orders.length} onChange={setTable} statusOptions={['', 'OPEN', 'COMPLETE', 'CANCELLED', 'REJECTED']} />
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
              <th>Variety</th>
              <th>Validity</th>
              <th>Trigger</th>
              <th>Order Price</th>
              <th>Avg Price</th>
              <th>Filled/Pending</th>
              <th>Status</th>
              <th>Message</th>
              <th>Source</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {page.items.map((order) => (
              <tr key={order.order_id}>
                <td>{formatOrderTime(order)}</td>
                <td className="mono">{order.order_id}</td>
                <td>{order.exchange}:{order.tradingsymbol}</td>
                <td><SidePill side={order.transaction_type} /></td>
                <td>{order.quantity}</td>
                <td>{order.order_type}</td>
                <td>{order.variety ?? '-'}</td>
                <td>{order.validity ?? '-'}</td>
                <td>{money(order.trigger_price)}</td>
                <td>{money(order.price)}</td>
                <td>{money(order.average_price)}</td>
                <td>{order.filled_quantity ?? 0}/{order.pending_quantity ?? 0}</td>
                <td><OrderStatusPill status={order.status} /></td>
                <td className="muted">{dash(order.status_message ?? '')}</td>
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
      <Pager page={page} state={table} onChange={setTable} />
      {orders.length === 0 && <EmptyState label="No synced orders" />}
    </section>
  );
}

function TradesView(props: {
  trades: ManagedTrade[];
  onStopLoss: (trade: ManagedTrade) => void;
  onTarget: (trade: ManagedTrade) => void;
  onExit: (trade: ManagedTrade) => void;
  onAMOExit: (trade: ManagedTrade) => void;
  onRemoveStopLoss: (trade: ManagedTrade) => void;
  onRemoveTarget: (trade: ManagedTrade) => void;
  onCancelEntry: (trade: ManagedTrade) => void;
}) {
  const [table, setTable] = useState<TableState>({ page: 1, pageSize: 25, status: '', symbol: '' });
  const trades = useMemo(() => props.trades.filter((trade) => matchesTableFilter(trade, table)), [props.trades, table]);
  const page = pageItems(trades, table);

  return (
    <section className="table-section">
      <PanelTitle icon={<CircleDollarSign />} title="Managed Trades" />
      <TableControls state={table} total={trades.length} onChange={setTable} statusOptions={['', 'OPEN', 'CLOSED']} />
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
            {page.items.map((trade) => (
              <tr key={trade.id}>
                <td className="mono">{trade.id}</td>
                <td>{trade.exchange}:{trade.tradingsymbol}</td>
                <td><SidePill side={trade.side} /></td>
                <td>{trade.quantity}</td>
                <td>{money(trade.entry_price)} <span className="muted">{trade.entry_status}</span></td>
                <td>{trade.stop_loss ? `${money(trade.stop_loss.trigger_price)} / ${money(trade.stop_loss.limit_price)}` : dash(trade.pending_stop_loss ? 'pending' : '')}</td>
                <td>{trade.target ? money(trade.target.price) : dash(trade.pending_target ? 'pending' : '')}</td>
                <td><TradeStatusPill status={trade.trade_status ?? 'OPEN'} /> {trade.exit_order_id && <span className="subline">exit {trade.exit_order_id}</span>}</td>
                <td className="row-actions">
                  {trade.trade_status !== 'CLOSED' && (
                    <>
                      <button className="icon-button" onClick={() => props.onStopLoss(trade)} title="Set stop-loss" aria-label="Set stop-loss"><Shield /></button>
                      {trade.stop_loss && <button className="icon-button danger" onClick={() => props.onRemoveStopLoss(trade)} title="Remove stop-loss" aria-label="Remove stop-loss"><XCircle /></button>}
                      <button className="icon-button" onClick={() => props.onTarget(trade)} title="Set target" aria-label="Set target"><Target /></button>
                      {trade.target && <button className="icon-button danger" onClick={() => props.onRemoveTarget(trade)} title="Remove target" aria-label="Remove target"><XCircle /></button>}
                      {trade.entry_status !== 'COMPLETE' && <button className="icon-button danger" onClick={() => props.onCancelEntry(trade)} title="Cancel entry" aria-label="Cancel entry"><Trash2 /></button>}
                      {trade.entry_status === 'COMPLETE' && !trade.exit_order_id && <button className="icon-button" onClick={() => props.onAMOExit(trade)} title="Queue AMO exit" aria-label="Queue AMO exit"><Clock3 /></button>}
                      <button className="icon-button danger" onClick={() => props.onExit(trade)} title="Exit trade" aria-label="Exit trade"><LogOut /></button>
                    </>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Pager page={page} state={table} onChange={setTable} />
      {trades.length === 0 && <EmptyState label="No managed trades" />}
    </section>
  );
}

function ConflictsView(props: {
  groups: PositionGroup[];
  orders: KiteOrder[];
  onLink: (group: PositionGroup) => void;
  onTakeOver: (group: PositionGroup) => void;
  onDetails: (group: PositionGroup) => void;
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
          onDetails: props.onDetails,
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
    onAMOExit?: (group: PositionGroup) => void;
    onRemoveStopLoss?: (group: PositionGroup) => void;
    onRemoveTarget?: (group: PositionGroup) => void;
    onUnlinkStopLoss?: (group: PositionGroup) => void;
    onUnlinkTarget?: (group: PositionGroup) => void;
    onApplyConversion?: (group: PositionGroup) => void;
    onTakeOver: (group: PositionGroup) => void;
    onDetails?: (group: PositionGroup) => void;
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
            <th>LTP</th>
            <th>P&L</th>
            <th>SL</th>
            <th>Target</th>
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
              <td>{money(group.last_price)}</td>
              <td><PnLValue value={group.last_price ? group.unrealized_pnl ?? 0 : undefined} /></td>
              <td>{groupProtectionLabel(group, 'stop-loss')}</td>
              <td>{groupProtectionLabel(group, 'target')}</td>
              <td>{group.local_quantity ?? 0}/{group.broker_quantity ?? 0}</td>
              <td><ManagementPill status={group.management_status} /></td>
              {!props.compact && <td><WarningChips warnings={group.warnings ?? []} /></td>}
              {!props.compact && (
                <td className="row-actions">
                  <button className="icon-button" onClick={() => props.actions?.onDetails?.(group)} title="Details" aria-label="Details"><Info /></button>
                  {group.management_status === 'UNMANAGED' ? (
                    <button className="icon-button" onClick={() => props.actions?.onTakeOver(group)} title="Take over" aria-label="Take over"><ArrowDownToLine /></button>
                  ) : props.conflictMode ? (
                    <button className="icon-button" onClick={() => props.actions?.onStopLoss(group)} title="Link external order" aria-label="Link external order"><Link2 /></button>
                  ) : (
                    <>
                      {group.converted_to_product && group.trade_ids.length === 1 && (
                        <button className="icon-button" onClick={() => props.actions?.onApplyConversion?.(group)} title="Apply product conversion" aria-label="Apply product conversion"><RefreshCw /></button>
                      )}
                      <button className="icon-button" onClick={() => props.actions?.onStopLoss(group)} title="Set stop-loss" aria-label="Set stop-loss"><Shield /></button>
                      {(group.stop_loss_count ?? 0) > 0 && <button className="icon-button danger" onClick={() => props.actions?.onRemoveStopLoss?.(group)} title="Remove stop-loss" aria-label="Remove stop-loss"><XCircle /></button>}
                      {(group.stop_loss_count ?? 0) > 0 && <button className="icon-button" onClick={() => props.actions?.onUnlinkStopLoss?.(group)} title="Unlink stop-loss locally" aria-label="Unlink stop-loss locally"><Unlink2 /></button>}
                      <button className="icon-button" onClick={() => props.actions?.onTarget(group)} title="Set target" aria-label="Set target"><Target /></button>
                      {(group.target_count ?? 0) > 0 && <button className="icon-button danger" onClick={() => props.actions?.onRemoveTarget?.(group)} title="Remove target" aria-label="Remove target"><XCircle /></button>}
                      {(group.target_count ?? 0) > 0 && <button className="icon-button" onClick={() => props.actions?.onUnlinkTarget?.(group)} title="Unlink target locally" aria-label="Unlink target locally"><Unlink2 /></button>}
                      {!group.exit_pending && <button className="icon-button" onClick={() => props.actions?.onAMOExit?.(group)} title="Queue AMO exit" aria-label="Queue AMO exit"><Clock3 /></button>}
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
  trades: ManagedTrade[];
  onClose: () => void;
  onRun: (label: string, fn: () => Promise<unknown>) => Promise<void>;
}) {
  const title =
    props.action.type === 'create-trade' ? 'New Trade' :
    props.action.type === 'stop-loss' ? 'Set Stop-Loss' :
    props.action.type === 'target' ? 'Set Target' :
    props.action.type === 'take-over' ? 'Take Over Position' :
    props.action.type === 'link-exit' ? 'Link External Exit' :
    'Position Details';

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
        {props.action.type === 'group-detail' && <GroupDetail group={props.action.group} trades={props.trades} orders={props.orders} />}
      </aside>
    </div>
  );
}

function GroupDetail(props: { group: PositionGroup; trades: ManagedTrade[]; orders: KiteOrder[] }) {
  const childTrades = props.trades.filter((trade) => props.group.trade_ids.includes(trade.id));
  const linkedOrderIDs = new Set(childTrades.flatMap((trade) => [trade.entry_order_id, trade.stop_order_id, trade.target_order_id, trade.exit_order_id].filter(Boolean) as string[]));
  const linkedOrders = props.orders.filter((order) => linkedOrderIDs.has(order.order_id));

  return (
    <div className="detail-stack">
      <div className="selected-box">
        <strong>{props.group.exchange}:{props.group.tradingsymbol}</strong>
        <span>{props.group.product} · {props.group.side || '-'} · qty {props.group.quantity}</span>
      </div>
      <DetailGrid rows={[
        ['Management', props.group.management_status],
        ['Source', props.group.creation_source],
        ['Local/Broker', `${props.group.local_quantity ?? 0}/${props.group.broker_quantity ?? 0}`],
        ['Average Entry', money(props.group.average_entry_price)],
        ['SL', groupProtectionLabel(props.group, 'stop-loss')],
        ['Target', groupProtectionLabel(props.group, 'target')],
        ['Exit Pending', props.group.exit_pending ? props.group.exit_order_id || 'yes' : 'no'],
      ]} />
      <section>
        <h4>Warnings</h4>
        <WarningChips warnings={props.group.warnings ?? []} />
      </section>
      <section>
        <h4>Child Trades</h4>
        {childTrades.length ? childTrades.map((trade) => (
          <div className="detail-row" key={trade.id}>
            <span className="mono">{trade.id}</span>
            <small>{trade.side} {trade.quantity} · entry {trade.entry_order_id}</small>
            <small>SL {dash(trade.stop_order_id ?? '')} · Target {dash(trade.target_order_id ?? '')} · Exit {dash(trade.exit_order_id ?? '')}</small>
          </div>
        )) : <p className="muted">No local child trade linked</p>}
      </section>
      <section>
        <h4>Linked Orders</h4>
        {linkedOrders.length ? linkedOrders.map((order) => (
          <div className="detail-row" key={order.order_id}>
            <span className="mono">{order.order_id}</span>
            <small>{order.transaction_type} {order.order_type} · {order.status} · {formatOrderTime(order)}</small>
            <small>price {money(order.price)} · trigger {money(order.trigger_price)} · avg {money(order.average_price)}</small>
          </div>
        )) : <p className="muted">No synced linked order snapshot</p>}
      </section>
    </div>
  );
}

function DetailGrid(props: { rows: Array<[string, React.ReactNode]> }) {
  return (
    <div className="detail-grid">
      {props.rows.map(([label, value]) => (
        <div key={label}>
          <span>{label}</span>
          <strong>{value}</strong>
        </div>
      ))}
    </div>
  );
}

function CreateTradeForm(props: { metadata?: Metadata; onRun: (label: string, fn: () => Promise<unknown>) => Promise<void> }) {
  const defaults = props.metadata?.runtime;
  const [form, setForm] = useState<CreateTradeRequest>({
    exchange: 'NFO',
    tradingsymbol: '',
    side: 'BUY',
    quantity: defaults?.default_quantity || 1,
    product: defaults?.default_product ?? 'MIS',
    order_type: 'MARKET',
    market_protection: defaults?.default_market_protection,
    protection: {
      stop_loss_points: defaults?.default_stop_loss_points || undefined,
      target_points: defaults?.default_target_points || undefined,
      sl_limit_offset: defaults?.default_sl_limit_offset || undefined,
    },
  });
  const [withProtection, setWithProtection] = useState(true);
  const [formError, setFormError] = useState('');
  const [optionExchange, setOptionExchange] = useState('NFO');
  const [underlyings, setUnderlyings] = useState<string[]>([]);
  const [underlying, setUnderlying] = useState('NIFTY');
  const [expiries, setExpiries] = useState<string[]>([]);
  const [expiry, setExpiry] = useState('');
  const [optionType, setOptionType] = useState('BOTH');
  const [contractsEachSide, setContractsEachSide] = useState(10);
  const [options, setOptions] = useState<OptionContract[]>([]);
  const [selectedOption, setSelectedOption] = useState('');
  const [selectedLotSize, setSelectedLotSize] = useState(0);
  const [optionStatus, setOptionStatus] = useState('');
  const [optionLoading, setOptionLoading] = useState(false);
  const [ltpLoading, setLTPLoading] = useState(false);
  const [underlyingQuote, setUnderlyingQuote] = useState<LTPResponse | null>(null);
  const [underlyingQuoteError, setUnderlyingQuoteError] = useState('');
  const effectiveWithProtection = withProtection || !!defaults?.require_order_protection;
  const protectionPreview = riskPreview(form, effectiveWithProtection);

  useEffect(() => {
    void loadOptionUnderlyings(optionExchange, false);
  }, [optionExchange]);

  useEffect(() => {
    if (underlying) void loadOptionExpiries(optionExchange, underlying);
  }, [optionExchange, underlying]);

  useEffect(() => {
    if (underlying) void loadUnderlyingQuote();
  }, [optionExchange, underlying]);

  function submit() {
    const body = { ...form, tradingsymbol: form.tradingsymbol.trim().toUpperCase() };
    if (!effectiveWithProtection) delete body.protection;
    if (body.order_type === 'MARKET') delete body.price;
    const validationError = validateCreateTradeForm(body, effectiveWithProtection, selectedLotSize);
    if (validationError) {
      setFormError(validationError);
      return;
    }
    setFormError('');
    return props.onRun('Created trade', () => api.createTrade(body));
  }

  function defaultProtection() {
    return {
      stop_loss_points: defaults?.default_stop_loss_points || undefined,
      target_points: defaults?.default_target_points || undefined,
      sl_limit_offset: defaults?.default_sl_limit_offset || undefined,
    };
  }

  function resetOptionSelection(exchange = optionExchange) {
    setOptions([]);
    setSelectedOption('');
    setSelectedLotSize(0);
    setFormError('');
    setOptionStatus('');
    setForm((current) => ({
      ...current,
      exchange,
      tradingsymbol: '',
      quantity: defaults?.default_quantity || 1,
      product: defaults?.default_product ?? 'MIS',
      order_type: 'MARKET',
      price: undefined,
      market_protection: defaults?.default_market_protection,
      protection: defaultProtection(),
    }));
  }

  function changeOptionExchange(value: string) {
    setOptionExchange(value);
    setUnderlyingQuote(null);
    setUnderlyingQuoteError('');
    setUnderlyings([]);
    setExpiries([]);
    setExpiry('');
    resetOptionSelection(value);
  }

  function changeUnderlying(value: string) {
    setUnderlying(value);
    setUnderlyingQuote(null);
    setUnderlyingQuoteError('');
    setExpiries([]);
    setExpiry('');
    resetOptionSelection(optionExchange);
  }

  function changeExpiry(value: string) {
    setExpiry(value);
    resetOptionSelection(optionExchange);
  }

  function changeOptionType(value: string) {
    setOptionType(value);
    resetOptionSelection(optionExchange);
  }

  async function syncOptionInstruments() {
    setOptionLoading(true);
    setOptionStatus('');
    try {
      await api.syncInstruments(optionExchange);
      await loadOptionUnderlyings(optionExchange, true);
      setOptionStatus(`Synced ${optionExchange} instruments`);
    } catch (err) {
      setOptionStatus(errorMessage(err));
    } finally {
      setOptionLoading(false);
    }
  }

  async function loadOptionUnderlyings(exchange: string, showStatus: boolean) {
    try {
      const values = await api.instrumentUnderlyings(exchange);
      setUnderlyings(values);
      if (values.length && !values.includes(underlying)) setUnderlying(values.includes('NIFTY') ? 'NIFTY' : values[0]);
      if (showStatus && values.length === 0) setOptionStatus('No option underlyings found');
    } catch (err) {
      if (showStatus) setOptionStatus(errorMessage(err));
    }
  }

  async function loadOptionExpiries(exchange: string, selectedUnderlying: string) {
    try {
      const values = await api.instrumentExpiries(exchange, selectedUnderlying);
      setExpiries(values);
      if (values.length && !values.includes(expiry)) setExpiry(values[0]);
    } catch {
      setExpiries([]);
    }
  }

  async function loadOptionContracts() {
    setOptionLoading(true);
    setOptionStatus('');
    try {
      const result = await api.optionContracts({
        exchange: optionExchange,
        underlying,
        expiry,
        types: optionType === 'BOTH' ? 'CE,PE' : optionType,
        range_points: DEFAULT_OPTION_RANGE_POINTS,
        contracts_each_side: contractsEachSide,
        product: defaults?.default_product ?? 'MIS',
      });
      setOptions(result.contracts);
      setSelectedOption(result.contracts[0]?.tradingsymbol ?? '');
      if (result.contracts[0]) selectOptionContract(result.contracts[0]);
      setOptionStatus(`${result.contracts.length} contracts · center ${money(result.center_strike)} ${result.center_source ? `(${result.center_source})` : ''}`);
    } catch (err) {
      setOptions([]);
      setOptionStatus(errorMessage(err));
    } finally {
      setOptionLoading(false);
    }
  }

  async function loadUnderlyingQuote() {
    const instrument = underlyingQuoteInstrument(optionExchange, underlying);
    if (!instrument) {
      setUnderlyingQuote(null);
      setUnderlyingQuoteError('Spot LTP unavailable');
      return;
    }
    setUnderlyingQuoteError('');
    try {
      const quote = await api.ltp(instrument.exchange, instrument.symbol);
      setUnderlyingQuote(quote);
    } catch (err) {
      setUnderlyingQuote(null);
      setUnderlyingQuoteError(errorMessage(err));
    }
  }

  function selectOption(value: string) {
    setSelectedOption(value);
    const contract = options.find((item) => item.tradingsymbol === value);
    if (contract) selectOptionContract(contract);
  }

  function selectOptionContract(contract: OptionContract) {
    setForm((current) => ({
      ...current,
      exchange: contract.exchange,
      tradingsymbol: contract.tradingsymbol,
      product: contract.product || defaults?.default_product || 'MIS',
      quantity: contract.default_quantity || contract.lot_size || current.quantity,
      order_type: 'MARKET',
      price: undefined,
      market_protection: defaults?.default_market_protection,
      protection: defaultProtection(),
    }));
    setSelectedLotSize(contract.lot_size || 0);
    setFormError('');
    void fetchEntryLTP(contract.exchange, contract.tradingsymbol, false);
  }

  function applyProtectionPreset(stopLossPoints: number, targetPoints: number) {
    setForm({
      ...form,
      protection: {
        ...form.protection,
        stop_loss_points: stopLossPoints,
        target_points: targetPoints,
        sl_limit_offset: form.protection?.sl_limit_offset ?? defaults?.default_sl_limit_offset,
      },
    });
    setWithProtection(true);
  }

  function changeOrderType(orderType: string) {
    setForm((current) => ({
      ...current,
      order_type: orderType,
      price: orderType === 'MARKET' ? undefined : current.price,
    }));
    if (orderType === 'LIMIT' && form.exchange && form.tradingsymbol) {
      void fetchEntryLTP(form.exchange, form.tradingsymbol, true);
    }
  }

  async function fetchEntryLTP(exchange = form.exchange, symbol = form.tradingsymbol, updateLimitPrice = false) {
    if (!exchange || !symbol) return;
    setLTPLoading(true);
    setFormError('');
    try {
      const result = await api.ltp(exchange, symbol);
      setForm((current) => ({
        ...current,
        price: updateLimitPrice ? result.last_price : current.price,
        protection: {
          ...current.protection,
          reference_price: result.last_price,
        },
      }));
    } catch (err) {
      setFormError(errorMessage(err));
    } finally {
      setLTPLoading(false);
    }
  }

  function useLTP(exchange = form.exchange, symbol = form.tradingsymbol) {
    return fetchEntryLTP(exchange, symbol, true);
  }

  return (
    <FormShell onSubmit={submit}>
      <div className="instrument-box">
        <div className="instrument-head">
          <strong>Options</strong>
          <button type="button" className="text-button" onClick={() => void syncOptionInstruments()} disabled={optionLoading}>
            {optionLoading ? 'Loading' : `Sync ${optionExchange}`}
          </button>
        </div>
        <div className="form-grid two">
          <Select label="Exchange" value={optionExchange} options={['NFO', 'BFO']} onChange={changeOptionExchange} />
          <Select label="Underlying" value={underlying} options={underlyings.length ? underlyings : ['NIFTY']} onChange={changeUnderlying} />
          <Select label="Expiry" value={expiry} options={expiries.length ? expiries : ['']} onChange={changeExpiry} />
          <Select label="Type" value={optionType} options={['BOTH', 'CE', 'PE']} onChange={changeOptionType} />
          <Input label="Contracts/Side" type="number" min={1} step={1} value={contractsEachSide} onChange={(value) => setContractsEachSide(Number(value))} />
        </div>
        <div className="selected-box compact">
          <strong>{underlying} spot</strong>
          <span>{underlyingQuote ? `${underlyingQuote.exchange}:${underlyingQuote.tradingsymbol} · ${money(underlyingQuote.last_price)}` : underlyingQuoteError ? 'LTP unavailable' : 'Loading LTP'}</span>
        </div>
        <button type="button" className="icon-text-button form-submit" onClick={() => void loadOptionContracts()} disabled={optionLoading || !underlying}>
          <RefreshCw />
          Load Options
        </button>
        {options.length > 0 && (
          <Select
            label="Option Contract"
            value={selectedOption}
            options={options.map((contract) => ({ value: contract.tradingsymbol, label: optionLabel(contract) }))}
            onChange={selectOption}
          />
        )}
        {optionStatus && <p className="form-hint">{optionStatus}</p>}
      </div>
      <Segmented label="Side" value={form.side} options={['BUY', 'SELL']} onChange={(side) => setForm({ ...form, side: side as Side })} />
      <Input label="Quantity" type="number" min={1} step={1} value={form.quantity} onChange={(quantity) => setForm({ ...form, quantity: Number(quantity) })} />
      {selectedLotSize > 0 && <p className="form-hint">Lot size {selectedLotSize}. Quantity must be a multiple of this value.</p>}
      <div className="selected-box">
        <strong>{form.tradingsymbol ? `${form.exchange}:${form.tradingsymbol}` : 'Select an option contract'}</strong>
        <span>{form.product}</span>
      </div>
      <Select label="Product" value={form.product} options={props.metadata?.enums.products ?? ['MIS', 'NRML']} onChange={(product) => setForm({ ...form, product })} />
      <Select label="Order Type" value={form.order_type} options={['MARKET', 'LIMIT']} onChange={changeOrderType} />
      {form.order_type === 'LIMIT' && (
        <div className="input-action-row">
          <Input label="Limit Price" type="number" min={0} step="0.05" value={form.price ?? ''} onChange={(price) => setForm({ ...form, price: Number(price) })} />
          <button type="button" className="text-button" onClick={() => void useLTP()} disabled={ltpLoading || !form.exchange || !form.tradingsymbol}>
            {ltpLoading ? 'Loading' : 'Use LTP'}
          </button>
        </div>
      )}
      <label className="check-row">
        <input type="checkbox" checked={effectiveWithProtection} disabled={!!defaults?.require_order_protection} onChange={(event) => setWithProtection(event.target.checked)} />
        <span>SL + Target Protection</span>
      </label>
      {effectiveWithProtection && (
        <>
          <div className="preset-row">
            <button type="button" onClick={() => applyProtectionPreset(20, 40)}>20 / 40</button>
            <button type="button" onClick={() => applyProtectionPreset(30, 60)}>30 / 60</button>
            <button type="button" onClick={() => applyProtectionPreset(50, 100)}>50 / 100</button>
          </div>
          {form.order_type === 'LIMIT' ? (
            <div className="selected-box compact">
              <strong>Risk basis</strong>
              <span>{form.price ? `Limit Price ${money(form.price)}` : 'Limit price will be used for SL/target preview'}</span>
            </div>
          ) : (
            <div className="input-action-row">
              <Input label="Est. Entry Price" type="number" min={0} step="0.05" value={form.protection?.reference_price ?? ''} onChange={(value) => setForm({ ...form, protection: { ...form.protection, reference_price: optionalNumber(value) } })} />
              <button type="button" className="text-button" onClick={() => void fetchEntryLTP()} disabled={ltpLoading || !form.exchange || !form.tradingsymbol}>
                {ltpLoading ? 'Loading' : 'Use LTP'}
              </button>
            </div>
          )}
          <div className="form-grid two">
            <Input label="SL Points" type="number" min={0} step="0.05" value={form.protection?.stop_loss_points ?? ''} onChange={(value) => setForm({ ...form, protection: { ...form.protection, stop_loss_points: optionalNumber(value) } })} />
            <Input label="Target Points" type="number" min={0} step="0.05" value={form.protection?.target_points ?? ''} onChange={(value) => setForm({ ...form, protection: { ...form.protection, target_points: optionalNumber(value) } })} />
            <Input label="SL Offset" type="number" min={0} step="0.05" value={form.protection?.sl_limit_offset ?? ''} onChange={(value) => setForm({ ...form, protection: { ...form.protection, sl_limit_offset: optionalNumber(value) } })} />
            <Input label="Trail SL By" type="number" min={0} step="0.05" value={form.protection?.trail_by ?? ''} onChange={(value) => setForm({ ...form, protection: { ...form.protection, trail_by: optionalNumber(value) } })} />
          </div>
          <p className="form-hint">Trail SL By is optional. If set, the backend moves the SL in your favor by this point gap while the position is active.</p>
          <RiskPreview preview={protectionPreview} orderType={form.order_type} />
        </>
      )}
      {formError && <p className="form-error">{formError}</p>}
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
      <Input label="Trigger Price" type="number" min={0} step="0.05" value={form.trigger_price || ''} onChange={(value) => setForm({ ...form, trigger_price: Number(value) })} />
      <Input label="Limit Price" type="number" min={0} step="0.05" value={form.limit_price || ''} onChange={(value) => setForm({ ...form, limit_price: Number(value) })} />
      <Input label="Trail SL By" type="number" min={0} step="0.05" value={form.trail_by ?? ''} onChange={(value) => setForm({ ...form, trail_by: Number(value) })} />
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
      <Input label="Target Price" type="number" min={0} step="0.05" value={form.price || ''} onChange={(value) => setForm({ price: Number(value) })} />
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
      <Input label="Entry Price" type="number" min={0} step="0.05" value={entryPrice} onChange={setEntryPrice} />
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

function RiskPreview(props: { preview?: RiskPreviewData; orderType: string }) {
  if (!props.preview) {
    return <p className="form-hint">Add an estimated entry price or use LTP to preview SL, target, and risk.</p>;
  }
  return (
    <div className="risk-preview">
      <DetailGrid rows={[
        ['Entry Basis', money(props.preview.referencePrice)],
        ['SL Trigger', money(props.preview.stopTrigger)],
        ['SL Limit', money(props.preview.stopLimit)],
        ['Target', money(props.preview.targetPrice)],
        ['Risk', signedMoney(-props.preview.riskAmount)],
        ['Reward', signedMoney(props.preview.rewardAmount)],
        ['R:R', props.preview.riskReward > 0 ? `1:${props.preview.riskReward.toFixed(2)}` : '-'],
      ]} />
      {props.orderType === 'LIMIT' && <p className="form-hint">Protection is kept pending until the limit entry completes.</p>}
    </div>
  );
}

function TableControls(props: { state: TableState; total: number; statusOptions: string[]; onChange: (state: TableState) => void }) {
  function update(patch: Partial<TableState>) {
    props.onChange({ ...props.state, ...patch, page: patch.page ?? 1 });
  }

  return (
    <div className="table-controls">
      <label className="field compact">
        <span>Symbol</span>
        <input value={props.state.symbol} onChange={(event) => update({ symbol: event.target.value })} placeholder="Filter" />
      </label>
      <label className="field compact">
        <span>Status</span>
        <select value={props.state.status} onChange={(event) => update({ status: event.target.value })}>
          {props.statusOptions.map((status) => <option key={status || 'all'} value={status}>{status || 'ALL'}</option>)}
        </select>
      </label>
      <label className="field compact">
        <span>Rows</span>
        <select value={props.state.pageSize} onChange={(event) => update({ pageSize: Number(event.target.value) })}>
          {[10, 25, 50, 100].map((size) => <option key={size} value={size}>{size}</option>)}
        </select>
      </label>
      <span className="table-total">{props.total} rows</span>
    </div>
  );
}

function PnLTile(props: { label: string; value: number }) {
  const tone = props.value > 0 ? 'positive' : props.value < 0 ? 'negative' : '';
  return (
    <div className={`pnl-tile ${tone}`}>
      <span>{props.label}</span>
      <strong>{signedMoney(props.value)}</strong>
    </div>
  );
}

function TabButton(props: { label: string; active: boolean; count: number; onClick: () => void }) {
  return (
    <button className={`tab-button ${props.active ? 'active' : ''}`} onClick={props.onClick}>
      <span>{props.label}</span>
      <strong>{props.count}</strong>
    </button>
  );
}

function ClosedTradeTable(props: { trades: ManagedTrade[]; orders: KiteOrder[] }) {
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Trade</th>
            <th>Symbol</th>
            <th>Side</th>
            <th>Qty</th>
            <th>Entry</th>
            <th>Exit</th>
            <th>P&L</th>
            <th>Closed</th>
            <th>Reason</th>
          </tr>
        </thead>
        <tbody>
          {props.trades.map((trade) => {
            const exitPrice = closedTradeExitPrice(trade, props.orders);
            const pnl = closedTradePnL(trade, props.orders);
            return (
              <tr key={trade.id}>
                <td className="mono">{trade.id}</td>
                <td>{trade.exchange}:{trade.tradingsymbol}</td>
                <td><SidePill side={trade.side} /></td>
                <td>{trade.quantity}</td>
                <td>{money(trade.entry_price)}</td>
                <td>{money(exitPrice)}</td>
                <td><PnLValue value={pnl} /></td>
                <td>{trade.closed_at ? formatDateTime(trade.closed_at) : '-'}</td>
                <td className="muted">{dash(trade.exit_reason ?? '')}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function PnLValue(props: { value?: number }) {
  if (props.value === undefined) return <span className="muted">-</span>;
  const tone = props.value > 0 ? 'positive' : props.value < 0 ? 'negative' : '';
  return <span className={`pnl-value ${tone}`}>{signedMoney(props.value)}</span>;
}

function Pager<T>(props: { page: PageResult<T>; state: TableState; onChange: (state: TableState) => void }) {
  if (props.page.totalPages <= 1) return null;
  return (
    <div className="pager">
      <button className="text-button" disabled={props.state.page <= 1} onClick={() => props.onChange({ ...props.state, page: props.state.page - 1 })}>Prev</button>
      <span>Page {props.page.page} / {props.page.totalPages}</span>
      <button className="text-button" disabled={props.state.page >= props.page.totalPages} onClick={() => props.onChange({ ...props.state, page: props.state.page + 1 })}>Next</button>
    </div>
  );
}

function Input(props: { label: string; value: string | number; type?: string; min?: number; step?: number | string; onChange: (value: string) => void }) {
  return (
    <label className="field">
      <span>{props.label}</span>
      <input type={props.type ?? 'text'} min={props.min} step={props.step} value={props.value} onChange={(event) => props.onChange(event.target.value)} />
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

function optionLabel(contract: OptionContract) {
  return `${contract.strike} ${contract.instrument_type} · ${contract.tradingsymbol} · lot ${contract.lot_size}`;
}

function groupProtectionLabel(group: PositionGroup, type: 'stop-loss' | 'target') {
  const count = type === 'stop-loss' ? group.stop_loss_count ?? 0 : group.target_count ?? 0;
  if (count === 0) return '-';
  if (type === 'stop-loss' && count === 1 && group.stop_loss) {
    return `${money(group.stop_loss.trigger_price)} / ${money(group.stop_loss.limit_price)}`;
  }
  if (type === 'target' && count === 1 && group.target) {
    return money(group.target.price);
  }
  return `${count} set`;
}

function latestSyncedAt(orders: KiteOrder[], positions: KitePosition[]) {
  const values = [
    ...orders.map((order) => order.synced_at),
    ...positions.map((position) => position.synced_at),
  ].filter(Boolean);
  values.sort((left, right) => Date.parse(right) - Date.parse(left));
  return values[0] ?? '';
}

function linkedTrade(trades: ManagedTrade[], group: PositionGroup) {
  return trades.find((trade) => group.trade_ids.includes(trade.id));
}

function mergeLivePositionsIntoGroups(groups: PositionGroup[], positions: KitePosition[]) {
  const merged = [...groups];
  const seen = new Set(merged.map((group) => group.id));
  for (const position of positions) {
    const id = positionGroupID(position.exchange, position.tradingsymbol, position.product);
    if (seen.has(id)) continue;
    const quantity = Math.abs(position.quantity);
    const pnlPercent = position.average_price && quantity ? (position.pnl ?? 0) / (position.average_price * quantity) * 100 : undefined;
    merged.push({
      id,
      exchange: position.exchange,
      tradingsymbol: position.tradingsymbol,
      product: position.product,
      side: position.quantity < 0 ? 'SELL' : position.quantity > 0 ? 'BUY' : '',
      quantity,
      local_quantity: 0,
      broker_quantity: quantity,
      average_entry_price: position.average_price,
      last_price: position.last_price,
      unrealized_pnl: position.pnl,
      pnl_percent: pnlPercent,
      market_synced_at: position.synced_at,
      trade_ids: [],
      trade_status: 'OPEN',
      creation_source: 'KITE_APP',
      management_status: 'UNMANAGED',
      created_at: position.synced_at,
      updated_at: position.synced_at,
    });
    seen.add(id);
  }
  return merged;
}

function positionGroupID(exchange: string, symbol: string, product: string) {
  return `${exchange}:${symbol}:${product}`.toUpperCase();
}

function underlyingQuoteInstrument(optionExchange: string, underlying: string) {
  const symbol = underlying.trim().toUpperCase();
  if (!symbol) return null;
  const indexMap: Record<string, { exchange: string; symbol: string }> = {
    NIFTY: { exchange: 'NSE', symbol: 'NIFTY 50' },
    BANKNIFTY: { exchange: 'NSE', symbol: 'NIFTY BANK' },
    FINNIFTY: { exchange: 'NSE', symbol: 'NIFTY FIN SERVICE' },
    MIDCPNIFTY: { exchange: 'NSE', symbol: 'NIFTY MID SELECT' },
    SENSEX: { exchange: 'BSE', symbol: 'SENSEX' },
  };
  if (indexMap[symbol]) return indexMap[symbol];
  if (['BANKEX', 'FOCIT', 'SENSEX50'].includes(symbol)) return null;
  return { exchange: optionExchange === 'BFO' ? 'BSE' : 'NSE', symbol };
}

function matchesTableFilter(item: KiteOrder | ManagedTrade | PositionGroup, state: TableState) {
  const symbol = state.symbol.trim().toUpperCase();
  if (symbol) {
    const haystack = [
      'exchange' in item ? item.exchange : '',
      'tradingsymbol' in item ? item.tradingsymbol : '',
      'product' in item ? item.product : '',
      'id' in item ? item.id : '',
      'order_id' in item ? item.order_id : '',
    ].join(':').toUpperCase();
    if (!haystack.includes(symbol)) return false;
  }
  if (!state.status) return true;
  const status =
    'status' in item ? item.status :
    'management_status' in item ? item.management_status :
    'trade_status' in item ? item.trade_status :
    '';
  return String(status).toUpperCase() === state.status.toUpperCase();
}

function pageItems<T>(items: T[], state: TableState): PageResult<T> {
  const total = items.length;
  const totalPages = Math.max(1, Math.ceil(total / state.pageSize));
  const page = Math.min(Math.max(1, state.page), totalPages);
  const start = (page - 1) * state.pageSize;
  return {
    items: items.slice(start, start + state.pageSize),
    page,
    totalPages,
    total,
  };
}

function isSyncResult(value: unknown): value is { synced_at: string } {
  return !!value && typeof value === 'object' && 'synced_at' in value;
}

function optionalNumber(value: string) {
  return value === '' ? undefined : Number(value);
}

function validateCreateTradeForm(body: CreateTradeRequest, withProtection: boolean, lotSize: number) {
  if (!body.exchange.trim() || !body.tradingsymbol.trim()) return 'Exchange and symbol are required.';
  if (!Number.isFinite(body.quantity) || body.quantity <= 0) return 'Quantity must be positive.';
  if (lotSize > 0 && body.quantity % lotSize !== 0) return `Quantity must be a multiple of lot size ${lotSize}.`;
  if (body.order_type === 'LIMIT' && (!Number.isFinite(body.price ?? 0) || (body.price ?? 0) <= 0)) {
    return 'Limit price must be positive.';
  }
  if (!withProtection || !body.protection) return '';

  const protection = body.protection;
  if (!protection.stop_loss_points || protection.stop_loss_points <= 0) return 'SL points are required for protected trades.';
  if (!protection.target_points || protection.target_points <= 0) return 'Target points are required for protected trades.';
  const values: Array<[string, number | undefined]> = [
    ['Estimated entry price', protection.reference_price],
    ['SL points', protection.stop_loss_points],
    ['Target points', protection.target_points],
    ['SL offset', protection.sl_limit_offset],
    ['Trail by', protection.trail_by],
  ];
  for (const [label, value] of values) {
    if (value !== undefined && (!Number.isFinite(value) || value < 0)) {
      return `${label} cannot be negative.`;
    }
  }

  const referencePrice = body.order_type === 'LIMIT' ? body.price || 0 : protection.reference_price || body.price || 0;
  if (referencePrice <= 0) return '';

  const stopLossPoints = protection.stop_loss_points ?? 0;
  const targetPoints = protection.target_points ?? 0;
  const slOffset = protection.sl_limit_offset ?? 0;
  if (body.side === 'BUY' && stopLossPoints > 0 && (referencePrice - stopLossPoints <= 0 || referencePrice - stopLossPoints - slOffset <= 0)) {
    return 'SL points/offset are too large for the entry price.';
  }
  if (body.side === 'SELL' && targetPoints > 0 && referencePrice - targetPoints <= 0) {
    return 'Target points are too large for the entry price.';
  }
  return '';
}

function riskPreview(body: CreateTradeRequest, withProtection: boolean): RiskPreviewData | undefined {
  const protection = body.protection;
  if (!withProtection || !protection) return undefined;
  const referencePrice = body.order_type === 'LIMIT' ? body.price || 0 : protection.reference_price || body.price || 0;
  const stopLossPoints = protection.stop_loss_points ?? 0;
  const targetPoints = protection.target_points ?? 0;
  if (referencePrice <= 0 || stopLossPoints <= 0 || targetPoints <= 0 || body.quantity <= 0) return undefined;
  const offset = protection.sl_limit_offset ?? 0;
  const isSell = body.side === 'SELL';
  const stopTrigger = isSell ? referencePrice + stopLossPoints : referencePrice - stopLossPoints;
  const stopLimit = isSell ? stopTrigger + offset : stopTrigger - offset;
  const targetPrice = isSell ? referencePrice - targetPoints : referencePrice + targetPoints;
  const riskAmount = stopLossPoints * body.quantity;
  const rewardAmount = targetPoints * body.quantity;
  return {
    referencePrice,
    stopTrigger,
    stopLimit,
    targetPrice,
    riskAmount,
    rewardAmount,
    riskReward: riskAmount > 0 ? rewardAmount / riskAmount : 0,
  };
}

function groupNeedsAttentionUI(group: PositionGroup) {
  return group.management_status === 'CONFLICT' ||
    group.management_status === 'UNMANAGED' ||
    group.management_status === 'PARTIALLY_MANAGED' ||
    (group.warnings?.length ?? 0) > 0;
}

function closedTradeExitPrice(trade: ManagedTrade, orders: KiteOrder[]) {
  if (!trade.exit_order_id) return undefined;
  const order = orders.find((item) => item.order_id === trade.exit_order_id);
  return order?.average_price || order?.price || undefined;
}

function closedTradePnL(trade: ManagedTrade, orders: KiteOrder[]) {
  const exitPrice = closedTradeExitPrice(trade, orders);
  if (!exitPrice || !trade.entry_price || !trade.quantity) return undefined;
  const difference = trade.side === 'SELL' ? trade.entry_price - exitPrice : exitPrice - trade.entry_price;
  return difference * trade.quantity;
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

function formatDateTime(value: string) {
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

function signedMoney(value: number) {
  const formatted = Math.abs(value).toLocaleString('en-IN', { maximumFractionDigits: 2 });
  if (value > 0) return `+${formatted}`;
  if (value < 0) return `-${formatted}`;
  return '0';
}

function dash(value: string) {
  return value || '-';
}

function errorMessage(err: unknown) {
  if (err instanceof ApiError) {
    if (err.body?.code === 'market_closed_amo_required') {
      return err.message;
    }
    return `${err.body?.code ?? err.status}: ${err.message}`;
  }
  if (err instanceof Error) return err.message;
  return 'Unexpected error';
}
