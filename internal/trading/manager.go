package trading

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"trading-system/internal/observability"
)

type Manager struct {
	broker             Broker
	store              Store
	orderStore         OrderStore
	positionStore      PositionStore
	logger             *slog.Logger
	mu                 sync.RWMutex
	trades             map[string]*ManagedTrade
	orders             map[string]*KiteOrder
	positions          map[string]*KitePosition
	positionsSynced    bool
	productConversions map[string]string
}

func NewManager(broker Broker) *Manager {
	manager, _ := NewManagerWithStore(broker, nil)
	return manager
}

func NewManagerWithStore(broker Broker, store Store) (*Manager, error) {
	return NewManagerWithStores(broker, store, nil)
}

func NewManagerWithStores(broker Broker, store Store, orderStore OrderStore) (*Manager, error) {
	return NewManagerWithAllStores(broker, store, orderStore, nil)
}

func NewManagerWithAllStores(broker Broker, store Store, orderStore OrderStore, positionStore PositionStore) (*Manager, error) {
	manager := &Manager{
		broker:             broker,
		store:              store,
		orderStore:         orderStore,
		positionStore:      positionStore,
		logger:             slog.Default(),
		trades:             make(map[string]*ManagedTrade),
		orders:             make(map[string]*KiteOrder),
		positions:          make(map[string]*KitePosition),
		productConversions: make(map[string]string),
	}

	if store != nil {
		trades, err := store.Load()
		if err != nil {
			return nil, err
		}
		for _, trade := range trades {
			if trade == nil || trade.ID == "" {
				continue
			}
			if trade.TradeStatus == "" {
				trade.TradeStatus = TradeStatusOpen
			}
			if trade.InitialQuantity == 0 {
				trade.InitialQuantity = trade.Quantity
			}
			manager.trades[trade.ID] = cloneTrade(trade)
		}
	}

	if orderStore != nil {
		orders, err := orderStore.LoadOrders()
		if err != nil {
			return nil, err
		}
		for _, order := range orders {
			if order == nil || order.OrderID == "" {
				continue
			}
			manager.orders[order.OrderID] = cloneKiteOrder(order)
		}
	}

	if positionStore != nil {
		positions, err := positionStore.LoadPositions()
		if err != nil {
			return nil, err
		}
		for _, position := range positions {
			if position == nil {
				continue
			}
			manager.positions[positionKey(position.Exchange, position.TradingSymbol, position.Product)] = cloneKitePosition(position)
		}
		manager.positionsSynced = true
	}

	return manager, nil
}

type CreateTradeRequest struct {
	Exchange         string                 `json:"exchange"`
	TradingSymbol    string                 `json:"tradingsymbol"`
	Side             string                 `json:"side"`
	Quantity         int                    `json:"quantity"`
	Product          string                 `json:"product"`
	OrderType        string                 `json:"order_type"`
	Price            float64                `json:"price,omitempty"`
	MarketProtection *int                   `json:"market_protection,omitempty"`
	Protection       *AutoProtectionRequest `json:"protection,omitempty"`
}

type AutoProtectionRequest struct {
	ReferencePrice float64 `json:"reference_price,omitempty"`
	StopLossPoints float64 `json:"stop_loss_points,omitempty"`
	TargetPoints   float64 `json:"target_points,omitempty"`
	TrailBy        float64 `json:"trail_by,omitempty"`
	SLLimitOffset  float64 `json:"sl_limit_offset,omitempty"`
}

type ImportTradeRequest struct {
	ID            string  `json:"id,omitempty"`
	Exchange      string  `json:"exchange"`
	TradingSymbol string  `json:"tradingsymbol"`
	Side          string  `json:"side"`
	Quantity      int     `json:"quantity"`
	Product       string  `json:"product"`
	EntryPrice    float64 `json:"entry_price,omitempty"`
	EntryOrderID  string  `json:"entry_order_id"`
}

func (m *Manager) Enter(ctx context.Context, req CreateTradeRequest) (*ManagedTrade, error) {
	side := strings.ToUpper(req.Side)
	if side != "BUY" && side != "SELL" {
		return nil, validationError("invalid_side", "side must be BUY or SELL")
	}
	if req.Quantity <= 0 {
		return nil, validationError("invalid_quantity", "quantity must be positive")
	}
	if req.Protection != nil {
		if err := validateAutoProtectionRequest(req.Exchange, side, req.Price, *req.Protection); err != nil {
			return nil, err
		}
	}
	if existing := m.activeOppositeSideGroup(req.Exchange, req.TradingSymbol, valueOr(req.Product, "MIS"), side); existing != nil {
		return nil, conflictError(
			"opposite_side_active_group",
			fmt.Sprintf("active %s group already exists for %s:%s %s; use exit/reduction flow instead of creating an opposite-side entry", existing.Side, existing.Exchange, existing.TradingSymbol, existing.Product),
		)
	}

	now := time.Now().UTC()
	orderID, err := m.broker.PlaceOrder(ctx, Order{
		Exchange:         req.Exchange,
		TradingSymbol:    req.TradingSymbol,
		TransactionType:  side,
		Quantity:         req.Quantity,
		Product:          valueOr(req.Product, "MIS"),
		OrderType:        valueOr(req.OrderType, "MARKET"),
		Variety:          "regular",
		Validity:         "DAY",
		Price:            req.Price,
		MarketProtection: req.MarketProtection,
		Tag:              LocalSystemOrderTag,
	})
	if err != nil {
		return nil, err
	}

	trade := &ManagedTrade{
		ID:              fmt.Sprintf("%s-%d", strings.ToLower(req.TradingSymbol), now.UnixNano()),
		Exchange:        req.Exchange,
		TradingSymbol:   req.TradingSymbol,
		Side:            side,
		Quantity:        req.Quantity,
		InitialQuantity: req.Quantity,
		Product:         valueOr(req.Product, "MIS"),
		EntryPrice:      initialEntryPrice(req),
		EntryOrderID:    orderID,
		EntryStatus:     initialEntryStatus(req),
		TradeStatus:     TradeStatusOpen,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if req.Protection != nil && shouldDeferProtection(req) {
		trade.PendingProtection = pendingProtection(*req.Protection)
	}

	m.mu.Lock()
	if existing, ok := m.trades[trade.ID]; ok {
		m.mu.Unlock()
		return nil, conflictError("duplicate_trade_id", fmt.Sprintf("trade id already exists: %s", existing.ID))
	}
	if existing := m.findByEntryOrderIDLocked(trade.EntryOrderID); existing != nil {
		m.mu.Unlock()
		return nil, conflictError("duplicate_entry_order_id", fmt.Sprintf("entry_order_id already exists on trade %s", existing.ID))
	}
	m.trades[trade.ID] = trade
	m.mu.Unlock()
	if err := m.persist(); err != nil {
		return nil, err
	}
	m.logTradeEvent(ctx, "trade_created", trade)

	if req.Protection != nil && !shouldDeferProtection(req) {
		return m.applyAutoProtection(ctx, trade.ID, req.Price, *req.Protection)
	}

	return cloneTrade(trade), nil
}

func (m *Manager) Import(ctx context.Context, req ImportTradeRequest) (*ManagedTrade, error) {
	side := strings.ToUpper(req.Side)
	if side != "BUY" && side != "SELL" {
		return nil, validationError("invalid_side", "side must be BUY or SELL")
	}
	if req.Quantity <= 0 {
		return nil, validationError("invalid_quantity", "quantity must be positive")
	}
	if req.EntryOrderID == "" {
		return nil, validationError("missing_entry_order_id", "entry_order_id is required")
	}

	now := time.Now().UTC()
	id := req.ID
	if id == "" {
		id = fmt.Sprintf("%s-%d", strings.ToLower(req.TradingSymbol), now.UnixNano())
	}
	trade := &ManagedTrade{
		ID:              id,
		Exchange:        req.Exchange,
		TradingSymbol:   req.TradingSymbol,
		Side:            side,
		Quantity:        req.Quantity,
		InitialQuantity: req.Quantity,
		Product:         valueOr(req.Product, "MIS"),
		EntryPrice:      req.EntryPrice,
		EntryOrderID:    req.EntryOrderID,
		EntryStatus:     OrderStatusComplete,
		TradeStatus:     TradeStatusOpen,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	m.mu.Lock()
	if existing, ok := m.trades[trade.ID]; ok {
		m.mu.Unlock()
		return nil, conflictError("duplicate_trade_id", fmt.Sprintf("trade id already exists: %s", existing.ID))
	}
	if existing := m.findByEntryOrderIDLocked(trade.EntryOrderID); existing != nil {
		m.mu.Unlock()
		return nil, conflictError("duplicate_entry_order_id", fmt.Sprintf("entry_order_id already exists on trade %s", existing.ID))
	}
	m.trades[trade.ID] = trade
	m.mu.Unlock()
	if err := m.persist(); err != nil {
		return nil, err
	}

	m.logTradeEvent(ctx, "trade_imported", trade)
	return cloneTrade(trade), nil
}

type StopLossRequest struct {
	TriggerPrice float64 `json:"trigger_price"`
	LimitPrice   float64 `json:"limit_price"`
	TrailBy      float64 `json:"trail_by,omitempty"`
}

func (m *Manager) AddStopLoss(ctx context.Context, id string, req StopLossRequest) (*ManagedTrade, error) {
	if req.TriggerPrice <= 0 || req.LimitPrice <= 0 {
		return nil, validationError("missing_stop_loss_prices", "trigger_price and limit_price are required")
	}

	trade, err := m.get(id)
	if err != nil {
		return nil, err
	}
	if err := ensureTradeOpen(trade); err != nil {
		return nil, err
	}
	if err := m.ensureTradeProductCurrent(trade); err != nil {
		return nil, err
	}

	exitSide := opposite(trade.Side)
	if err := validateStopAgainstEntry(trade, req.TriggerPrice); err != nil {
		return nil, err
	}
	if err := validateStopLimit(exitSide, req.TriggerPrice, req.LimitPrice); err != nil {
		return nil, err
	}
	if shouldDeferRiskOrder(trade) {
		result, err := m.setPendingStopLoss(id, req)
		if err == nil {
			m.logTradeEvent(ctx, "stop_loss_pending", result)
		}
		return result, err
	}
	orderID := trade.StopOrderID
	action := "created"
	if orderID == "" {
		orderID, err = m.broker.PlaceOrder(ctx, Order{
			Exchange:        trade.Exchange,
			TradingSymbol:   trade.TradingSymbol,
			TransactionType: exitSide,
			Quantity:        trade.Quantity,
			Product:         trade.Product,
			OrderType:       "SL",
			Variety:         "regular",
			Validity:        "DAY",
			Price:           req.LimitPrice,
			TriggerPrice:    req.TriggerPrice,
			Tag:             LocalSystemOrderTag,
		})
		if err != nil {
			return nil, err
		}
	} else {
		action = "modified"
		err = m.broker.ModifyOrder(ctx, "regular", orderID, map[string]string{
			"order_type":    "SL",
			"trigger_price": money(req.TriggerPrice),
			"price":         money(req.LimitPrice),
		})
		if err != nil {
			return nil, err
		}
	}

	m.mu.Lock()
	current := m.trades[id]
	current.StopOrderID = orderID
	current.StopOrderStatus = OrderStatusOpen
	current.StopLoss = &StopLoss{
		TriggerPrice: req.TriggerPrice,
		LimitPrice:   req.LimitPrice,
		TrailBy:      req.TrailBy,
	}
	current.PendingStopLoss = nil
	current.UpdatedAt = time.Now().UTC()
	result := cloneTrade(current)
	m.mu.Unlock()
	if err := m.persist(); err != nil {
		return nil, err
	}
	m.logTradeEvent(ctx, "stop_loss_"+action, result)
	return result, nil
}

type TargetRequest struct {
	Price float64 `json:"price"`
}

func (m *Manager) AddTarget(ctx context.Context, id string, req TargetRequest) (*ManagedTrade, error) {
	if req.Price <= 0 {
		return nil, validationError("missing_price", "price is required")
	}

	trade, err := m.get(id)
	if err != nil {
		return nil, err
	}
	if err := ensureTradeOpen(trade); err != nil {
		return nil, err
	}
	if err := m.ensureTradeProductCurrent(trade); err != nil {
		return nil, err
	}
	if err := validateTargetAgainstEntry(trade, req.Price); err != nil {
		return nil, err
	}
	if shouldDeferRiskOrder(trade) {
		result, err := m.setPendingTarget(id, req)
		if err == nil {
			m.logTradeEvent(ctx, "target_pending", result)
		}
		return result, err
	}

	orderID := trade.TargetOrderID
	action := "created"
	if orderID == "" {
		orderID, err = m.broker.PlaceOrder(ctx, Order{
			Exchange:        trade.Exchange,
			TradingSymbol:   trade.TradingSymbol,
			TransactionType: opposite(trade.Side),
			Quantity:        trade.Quantity,
			Product:         trade.Product,
			OrderType:       "LIMIT",
			Variety:         "regular",
			Validity:        "DAY",
			Price:           req.Price,
			Tag:             LocalSystemOrderTag,
		})
		if err != nil {
			return nil, err
		}
	} else {
		action = "modified"
		err = m.broker.ModifyOrder(ctx, "regular", orderID, map[string]string{
			"order_type": "LIMIT",
			"price":      money(req.Price),
		})
		if err != nil {
			return nil, err
		}
	}

	m.mu.Lock()
	current := m.trades[id]
	current.TargetOrderID = orderID
	current.TargetOrderStatus = OrderStatusOpen
	current.Target = &Target{Price: req.Price}
	current.PendingTarget = nil
	current.UpdatedAt = time.Now().UTC()
	result := cloneTrade(current)
	m.mu.Unlock()
	if err := m.persist(); err != nil {
		return nil, err
	}
	m.logTradeEvent(ctx, "target_"+action, result)
	return result, nil
}

func (m *Manager) applyAutoProtection(ctx context.Context, id string, orderPrice float64, req AutoProtectionRequest) (*ManagedTrade, error) {
	if req.StopLossPoints <= 0 && req.TargetPoints <= 0 {
		return m.get(id)
	}

	trade, err := m.get(id)
	if err != nil {
		return nil, err
	}
	if err := ensureTradeOpen(trade); err != nil {
		return nil, err
	}

	referencePrice, err := m.referencePrice(ctx, trade, orderPrice, req.ReferencePrice)
	if err != nil {
		return nil, err
	}
	if err := m.setEntryPriceIfMissing(id, referencePrice); err != nil {
		return nil, err
	}
	trade.EntryPrice = referencePrice

	current := trade
	if req.StopLossPoints > 0 {
		triggerPrice, limitPrice := stopPrices(trade.Exchange, trade.Side, referencePrice, req.StopLossPoints, req.SLLimitOffset)
		current, err = m.AddStopLoss(ctx, id, StopLossRequest{
			TriggerPrice: triggerPrice,
			LimitPrice:   limitPrice,
			TrailBy:      req.TrailBy,
		})
		if err != nil {
			return nil, err
		}
	}

	if req.TargetPoints > 0 {
		current, err = m.AddTarget(ctx, id, TargetRequest{
			Price: targetPrice(trade.Side, referencePrice, req.TargetPoints),
		})
		if err != nil {
			return nil, err
		}
	}

	return current, nil
}

func (m *Manager) referencePrice(ctx context.Context, trade *ManagedTrade, orderPrice, requestedReference float64) (float64, error) {
	if requestedReference > 0 {
		return requestedReference, nil
	}
	if orderPrice > 0 {
		return orderPrice, nil
	}
	ltp, err := m.broker.LTP(ctx, trade.Exchange, trade.TradingSymbol)
	if err != nil {
		return 0, fmt.Errorf("reference_price is required when LTP cannot be fetched: %w", err)
	}
	return ltp, nil
}

func (m *Manager) RemoveStopLoss(ctx context.Context, id string) (*ManagedTrade, error) {
	trade, err := m.get(id)
	if err != nil {
		return nil, err
	}
	if trade.StopOrderID != "" {
		if err := m.broker.CancelOrder(ctx, "regular", trade.StopOrderID); err != nil {
			return nil, err
		}
	}

	m.mu.Lock()
	current := m.trades[id]
	current.StopOrderID = ""
	current.StopOrderStatus = ""
	current.StopLoss = nil
	current.PendingStopLoss = nil
	current.UpdatedAt = time.Now().UTC()
	result := cloneTrade(current)
	m.mu.Unlock()
	if err := m.persist(); err != nil {
		return nil, err
	}
	m.logTradeEvent(ctx, "stop_loss_removed", result)
	return result, nil
}

func (m *Manager) RemoveTarget(ctx context.Context, id string) (*ManagedTrade, error) {
	trade, err := m.get(id)
	if err != nil {
		return nil, err
	}
	if err := ensureTradeOpen(trade); err != nil {
		return nil, err
	}
	if trade.TargetOrderID != "" {
		if err := m.broker.CancelOrder(ctx, "regular", trade.TargetOrderID); err != nil {
			return nil, err
		}
	}

	m.mu.Lock()
	current := m.trades[id]
	current.TargetOrderID = ""
	current.TargetOrderStatus = ""
	current.Target = nil
	current.PendingTarget = nil
	current.UpdatedAt = time.Now().UTC()
	result := cloneTrade(current)
	m.mu.Unlock()
	if err := m.persist(); err != nil {
		return nil, err
	}
	m.logTradeEvent(ctx, "target_removed", result)
	return result, nil
}

func (m *Manager) Exit(ctx context.Context, id string) (*ManagedTrade, error) {
	trade, err := m.get(id)
	if err != nil {
		return nil, err
	}
	if err := ensureTradeOpen(trade); err != nil {
		return nil, err
	}
	if err := m.ensureTradeProductCurrent(trade); err != nil {
		return nil, err
	}

	if trade.StopOrderID != "" {
		if err := m.broker.CancelOrder(ctx, "regular", trade.StopOrderID); err != nil {
			return nil, err
		}
	}
	if trade.TargetOrderID != "" {
		if err := m.broker.CancelOrder(ctx, "regular", trade.TargetOrderID); err != nil {
			return nil, err
		}
	}

	exitOrderID, err := m.broker.PlaceOrder(ctx, Order{
		Exchange:         trade.Exchange,
		TradingSymbol:    trade.TradingSymbol,
		TransactionType:  opposite(trade.Side),
		Quantity:         trade.Quantity,
		Product:          trade.Product,
		OrderType:        "MARKET",
		Variety:          "regular",
		Validity:         "DAY",
		MarketProtection: intPtr(-1),
		Tag:              LocalSystemOrderTag,
	})
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	current := m.trades[id]
	current.ExitOrderID = exitOrderID
	current.TradeStatus = TradeStatusClosed
	current.ExitReason = ExitReasonManual
	now := time.Now().UTC()
	current.ClosedAt = &now
	if current.StopOrderID != "" {
		current.StopOrderStatus = OrderStatusCancelled
	}
	if current.TargetOrderID != "" {
		current.TargetOrderStatus = OrderStatusCancelled
	}
	current.StopLoss = nil
	current.Target = nil
	current.PendingProtection = nil
	current.PendingStopLoss = nil
	current.PendingTarget = nil
	current.UpdatedAt = time.Now().UTC()
	result := cloneTrade(current)
	m.mu.Unlock()
	if err := m.persist(); err != nil {
		return nil, err
	}
	m.logTradeEvent(ctx, "trade_manual_exit", result)
	return result, nil
}

func (m *Manager) List() []*ManagedTrade {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*ManagedTrade, 0, len(m.trades))
	for _, trade := range m.trades {
		out = append(out, cloneTrade(trade))
	}
	return out
}

func (m *Manager) ListGroups() []*PositionGroup {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.positionGroupsLocked()
}

func (m *Manager) ListSyncedOrders() []*KiteOrder {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*KiteOrder, 0, len(m.orders))
	for _, order := range m.orders {
		out = append(out, cloneKiteOrder(order))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].OrderID < out[j].OrderID
	})
	return out
}

func (m *Manager) ListSyncedPositions() []*KitePosition {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*KitePosition, 0, len(m.positions))
	for _, position := range m.positions {
		out = append(out, cloneKitePosition(position))
	}
	sort.Slice(out, func(i, j int) bool {
		return positionKey(out[i].Exchange, out[i].TradingSymbol, out[i].Product) < positionKey(out[j].Exchange, out[j].TradingSymbol, out[j].Product)
	})
	return out
}

func (m *Manager) SyncKite(ctx context.Context) (*SyncResult, error) {
	orders, err := m.broker.Orders(ctx)
	if err != nil {
		return nil, err
	}
	positions, err := m.broker.Positions(ctx)
	if err != nil {
		return nil, err
	}

	syncedAt := time.Now().UTC()
	result := &SyncResult{SyncedAt: syncedAt}
	nextOrders := make(map[string]*KiteOrder, len(orders))
	for _, order := range orders {
		order.SyncedAt = syncedAt
		order.CreationSource = creationSourceFromTag(order.Tag)
		nextOrders[order.OrderID] = cloneKiteOrder(&order)
		result.OrdersSynced++
		if order.CreationSource == CreationSourceLocalSystem {
			result.LocalSystemOrders++
		} else {
			result.ExternalOrders++
		}
	}
	nextPositions := make(map[string]*KitePosition, len(positions))
	for _, position := range positions {
		syncedPosition := &KitePosition{
			Exchange:      position.Exchange,
			TradingSymbol: position.TradingSymbol,
			Product:       position.Product,
			Quantity:      position.Quantity,
			SyncedAt:      syncedAt,
		}
		nextPositions[positionKey(position.Exchange, position.TradingSymbol, position.Product)] = syncedPosition
		result.PositionsSynced++
	}

	m.mu.Lock()
	previousPositions := m.positions
	countPositionChanges(m.positions, nextPositions, result)
	productConversions := retainProductConversions(m.productConversions, nextPositions)
	for fromKey, toKey := range detectProductConversions(m.positions, nextPositions) {
		productConversions[fromKey] = toKey
	}
	result.ProductConversionsDetected = len(productConversions)
	m.orders = nextOrders
	m.positions = nextPositions
	m.positionsSynced = true
	m.productConversions = productConversions
	m.mu.Unlock()
	m.reconcileSyncedPositions(ctx, previousPositions, nextPositions, productConversions)
	if err := m.persistOrders(); err != nil {
		return nil, err
	}
	if err := m.persistPositions(); err != nil {
		return nil, err
	}

	if m.logger != nil {
		m.logger.InfoContext(ctx, "kite_orders_synced",
			"request_id", observability.RequestID(ctx),
			"orders_synced", result.OrdersSynced,
			"positions_synced", result.PositionsSynced,
			"product_conversions_detected", result.ProductConversionsDetected,
			"local_system_orders", result.LocalSystemOrders,
			"external_orders", result.ExternalOrders,
		)
	}
	return result, nil
}

func (m *Manager) SyncKiteLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			syncCtx, _ := observability.EnsureRequestID(ctx, "sync-"+observability.NewRequestID())
			if _, err := m.SyncKite(syncCtx); err != nil && m.logger != nil {
				m.logger.WarnContext(syncCtx, "kite_sync_failed",
					"request_id", observability.RequestID(syncCtx),
					"error", err,
				)
			}
		}
	}
}

func (m *Manager) reconcileSyncedPositions(ctx context.Context, previous, next map[string]*KitePosition, productConversions map[string]string) {
	for key, oldPosition := range previous {
		if oldPosition == nil || oldPosition.Quantity == 0 {
			continue
		}
		newPosition := next[key]
		newQuantity := 0
		if newPosition != nil {
			newQuantity = newPosition.Quantity
		}
		if newQuantity == oldPosition.Quantity {
			continue
		}
		if _, converted := productConversions[key]; converted {
			m.logProductConversionDetected(ctx, oldPosition, next[productConversions[key]])
			continue
		}

		trades := m.openTradesForPosition(oldPosition.Exchange, oldPosition.TradingSymbol, oldPosition.Product)
		if len(trades) != 1 {
			m.logPositionReconciliationSkipped(ctx, oldPosition, len(trades))
			continue
		}
		trade := trades[0]
		switch {
		case newQuantity == 0:
			m.closeExternallyFlattenedTrade(ctx, trade)
		case sameSignedQuantity(oldPosition.Quantity, newQuantity) && absInt(newQuantity) < absInt(oldPosition.Quantity):
			m.reduceExternallyClosedTrade(ctx, trade, absInt(newQuantity))
		}
	}
}

func (m *Manager) logProductConversionDetected(ctx context.Context, from, to *KitePosition) {
	if m.logger == nil || from == nil || to == nil {
		return
	}
	m.logger.WarnContext(ctx, "product_conversion_detected",
		"request_id", observability.RequestID(ctx),
		"exchange", from.Exchange,
		"tradingsymbol", from.TradingSymbol,
		"from_product", from.Product,
		"to_product", to.Product,
		"quantity", absInt(to.Quantity),
	)
}

func (m *Manager) openTradesForPosition(exchange, symbol, product string) []*ManagedTrade {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var trades []*ManagedTrade
	for _, trade := range m.trades {
		if isTradeClosed(trade) || !samePositionGroup(trade.Exchange, trade.TradingSymbol, trade.Product, exchange, symbol, product) {
			continue
		}
		trades = append(trades, cloneTrade(trade))
	}
	return trades
}

func (m *Manager) closeExternallyFlattenedTrade(ctx context.Context, trade *ManagedTrade) {
	stopStatus := trade.StopOrderStatus
	if trade.StopOrderID != "" && !isTerminalOrderStatus(stopStatus) {
		if err := m.broker.CancelOrder(ctx, "regular", trade.StopOrderID); err == nil {
			stopStatus = OrderStatusCancelled
		}
	}
	targetStatus := trade.TargetOrderStatus
	if trade.TargetOrderID != "" && !isTerminalOrderStatus(targetStatus) {
		if err := m.broker.CancelOrder(ctx, "regular", trade.TargetOrderID); err == nil {
			targetStatus = OrderStatusCancelled
		}
	}
	_ = m.closeTrade(ctx, trade.ID, ExitReasonManualExternal, stopStatus, targetStatus)
}

func (m *Manager) reduceExternallyClosedTrade(ctx context.Context, trade *ManagedTrade, remainingQuantity int) {
	if remainingQuantity <= 0 || remainingQuantity >= trade.Quantity {
		return
	}
	fields := map[string]string{"quantity": strconv.Itoa(remainingQuantity)}
	if trade.StopOrderID != "" && !isTerminalOrderStatus(trade.StopOrderStatus) {
		if err := m.broker.ModifyOrder(ctx, "regular", trade.StopOrderID, fields); err != nil {
			m.logPositionProtectionResizeFailed(ctx, trade, trade.StopOrderID, remainingQuantity, err)
		}
	}
	if trade.TargetOrderID != "" && !isTerminalOrderStatus(trade.TargetOrderStatus) {
		if err := m.broker.ModifyOrder(ctx, "regular", trade.TargetOrderID, fields); err != nil {
			m.logPositionProtectionResizeFailed(ctx, trade, trade.TargetOrderID, remainingQuantity, err)
		}
	}

	m.mu.Lock()
	current := m.trades[trade.ID]
	if current != nil && !isTradeClosed(current) {
		current.Quantity = remainingQuantity
		current.UpdatedAt = time.Now().UTC()
	}
	result := cloneTrade(current)
	m.mu.Unlock()
	_ = m.persist()
	m.logTradeEvent(ctx, "trade_partial_external_exit", result)
}

func (m *Manager) logPositionReconciliationSkipped(ctx context.Context, position *KitePosition, openTrades int) {
	if m.logger == nil {
		return
	}
	m.logger.WarnContext(ctx, "position_reconciliation_skipped",
		"request_id", observability.RequestID(ctx),
		"exchange", position.Exchange,
		"tradingsymbol", position.TradingSymbol,
		"product", position.Product,
		"open_trades", openTrades,
		"reason", "position mapping is not unambiguous",
	)
}

func (m *Manager) logPositionProtectionResizeFailed(ctx context.Context, trade *ManagedTrade, orderID string, quantity int, err error) {
	if m.logger == nil {
		return
	}
	m.logger.WarnContext(ctx, "position_protection_resize_failed",
		"request_id", observability.RequestID(ctx),
		"trade_id", trade.ID,
		"order_id", orderID,
		"quantity", quantity,
		"error", err,
	)
}

func (m *Manager) TrailStops(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pollCtx, _ := observability.EnsureRequestID(ctx, "poll-"+observability.NewRequestID())
			m.trailOnce(pollCtx)
		}
	}
}

func (m *Manager) trailOnce(ctx context.Context) {
	for _, trade := range m.List() {
		if isTradeClosed(trade) {
			continue
		}
		m.tryApplyPendingProtection(ctx, trade)
		m.reconcileExternalPosition(ctx, trade)
		trade, err := m.get(trade.ID)
		if err != nil || isTradeClosed(trade) {
			continue
		}
		m.reconcileExitOrders(ctx, trade)
		trade, err = m.get(trade.ID)
		if err != nil || isTradeClosed(trade) {
			continue
		}

		if trade.StopLoss == nil || trade.StopLoss.TrailBy <= 0 || trade.StopOrderID == "" {
			continue
		}

		ltp, err := m.broker.LTP(ctx, trade.Exchange, trade.TradingSymbol)
		if err != nil {
			continue
		}

		nextTrigger, nextLimit, ok := nextTrailingStop(trade, ltp)
		if !ok {
			continue
		}

		fields := map[string]string{
			"order_type":    "SL",
			"trigger_price": money(nextTrigger),
			"price":         money(nextLimit),
		}
		if err := m.broker.ModifyOrder(ctx, "regular", trade.StopOrderID, fields); err != nil {
			continue
		}

		m.mu.Lock()
		current := m.trades[trade.ID]
		current.StopLoss.TriggerPrice = nextTrigger
		current.StopLoss.LimitPrice = nextLimit
		if current.Side == "BUY" {
			current.StopLoss.HighestLTP = max(current.StopLoss.HighestLTP, ltp)
		} else {
			current.StopLoss.LowestLTP = minNonZero(current.StopLoss.LowestLTP, ltp)
		}
		current.UpdatedAt = time.Now().UTC()
		m.mu.Unlock()
		_ = m.persist()
	}
}

func (m *Manager) reconcileExitOrders(ctx context.Context, trade *ManagedTrade) {
	if isTradeClosed(trade) || (trade.StopOrderID == "" && trade.TargetOrderID == "") {
		return
	}

	stopStatus := trade.StopOrderStatus
	var stopDetails *OrderDetails
	if trade.StopOrderID != "" {
		if details, err := m.broker.OrderDetails(ctx, "regular", trade.StopOrderID); err == nil && details != nil {
			stopDetails = details
			stopStatus = details.Status
		}
	}
	targetStatus := trade.TargetOrderStatus
	var targetDetails *OrderDetails
	if trade.TargetOrderID != "" {
		if details, err := m.broker.OrderDetails(ctx, "regular", trade.TargetOrderID); err == nil && details != nil {
			targetDetails = details
			targetStatus = details.Status
		}
	}

	stopComplete := strings.EqualFold(stopStatus, OrderStatusComplete)
	targetComplete := strings.EqualFold(targetStatus, OrderStatusComplete)

	switch {
	case stopComplete && targetComplete:
		_ = m.closeTrade(ctx, trade.ID, ExitReasonBothCompleted, stopStatus, targetStatus)
	case stopComplete:
		if trade.TargetOrderID != "" && !isTerminalOrderStatus(targetStatus) {
			_ = m.broker.CancelOrder(ctx, "regular", trade.TargetOrderID)
			targetStatus = OrderStatusCancelled
		}
		_ = m.closeTrade(ctx, trade.ID, ExitReasonStopLoss, stopStatus, targetStatus)
	case targetComplete:
		if trade.StopOrderID != "" && !isTerminalOrderStatus(stopStatus) {
			_ = m.broker.CancelOrder(ctx, "regular", trade.StopOrderID)
			stopStatus = OrderStatusCancelled
		}
		_ = m.closeTrade(ctx, trade.ID, ExitReasonTarget, stopStatus, targetStatus)
	default:
		_ = m.reconcileOpenExitOrderDetails(ctx, trade.ID, stopDetails, targetDetails)
	}
}

func (m *Manager) reconcileExternalPosition(ctx context.Context, trade *ManagedTrade) {
	if isTradeClosed(trade) || !strings.EqualFold(trade.EntryStatus, OrderStatusComplete) {
		return
	}

	positions, err := m.broker.Positions(ctx)
	if err != nil {
		return
	}
	if positionQuantity(positions, trade.Exchange, trade.TradingSymbol, trade.Product) != 0 {
		return
	}

	stopStatus := trade.StopOrderStatus
	if trade.StopOrderID != "" && !isTerminalOrderStatus(stopStatus) {
		_ = m.broker.CancelOrder(ctx, "regular", trade.StopOrderID)
		stopStatus = OrderStatusCancelled
	}
	targetStatus := trade.TargetOrderStatus
	if trade.TargetOrderID != "" && !isTerminalOrderStatus(targetStatus) {
		_ = m.broker.CancelOrder(ctx, "regular", trade.TargetOrderID)
		targetStatus = OrderStatusCancelled
	}
	_ = m.closeTrade(ctx, trade.ID, ExitReasonManualExternal, stopStatus, targetStatus)
}

func (m *Manager) tryApplyPendingProtection(ctx context.Context, trade *ManagedTrade) {
	if trade.PendingProtection == nil && trade.PendingStopLoss == nil && trade.PendingTarget == nil {
		return
	}
	if trade.EntryOrderID == "" {
		return
	}

	status, err := m.broker.OrderStatus(ctx, "regular", trade.EntryOrderID)
	if err != nil || !strings.EqualFold(status, "COMPLETE") {
		if status != "" {
			_ = m.updateEntryStatus(trade.ID, status)
		}
		return
	}
	_ = m.updateEntryStatus(trade.ID, "COMPLETE")
	trade.EntryStatus = "COMPLETE"

	if trade.PendingProtection != nil {
		protection := AutoProtectionRequest{
			ReferencePrice: trade.PendingProtection.ReferencePrice,
			StopLossPoints: trade.PendingProtection.StopLossPoints,
			TargetPoints:   trade.PendingProtection.TargetPoints,
			TrailBy:        trade.PendingProtection.TrailBy,
			SLLimitOffset:  trade.PendingProtection.SLLimitOffset,
		}
		if _, err := m.applyAutoProtection(ctx, trade.ID, trade.EntryPrice, protection); err != nil {
			return
		}
		_ = m.clearPendingProtection(trade.ID)
	}
	if trade.PendingStopLoss != nil {
		if _, err := m.AddStopLoss(ctx, trade.ID, StopLossRequest{
			TriggerPrice: trade.PendingStopLoss.TriggerPrice,
			LimitPrice:   trade.PendingStopLoss.LimitPrice,
			TrailBy:      trade.PendingStopLoss.TrailBy,
		}); err != nil {
			return
		}
	}
	if trade.PendingTarget != nil {
		if _, err := m.AddTarget(ctx, trade.ID, TargetRequest{Price: trade.PendingTarget.Price}); err != nil {
			return
		}
	}
}

func nextTrailingStop(trade *ManagedTrade, ltp float64) (float64, float64, bool) {
	sl := trade.StopLoss
	if trade.Side == "BUY" {
		reference := max(sl.HighestLTP, ltp)
		candidate := reference - sl.TrailBy
		if candidate <= sl.TriggerPrice {
			return 0, 0, false
		}
		return candidate, candidate - defaultSLLimitOffset(trade.Exchange), true
	}

	reference := minNonZero(sl.LowestLTP, ltp)
	candidate := reference + sl.TrailBy
	if candidate >= sl.TriggerPrice {
		return 0, 0, false
	}
	return candidate, candidate + defaultSLLimitOffset(trade.Exchange), true
}

func (m *Manager) get(id string) (*ManagedTrade, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	trade, ok := m.trades[id]
	if !ok {
		return nil, notFoundError("trade_not_found", "trade not found")
	}
	return cloneTrade(trade), nil
}

func (m *Manager) ensureTradeProductCurrent(trade *ManagedTrade) error {
	m.mu.RLock()
	toKey, converted := m.productConversions[positionKey(trade.Exchange, trade.TradingSymbol, trade.Product)]
	m.mu.RUnlock()
	if !converted {
		return nil
	}
	return conflictError(
		"product_conversion_detected",
		fmt.Sprintf("position product changed from %s to %s; sync product conversion before modifying protection or exiting", trade.Product, productFromPositionKey(toKey)),
	)
}

func (m *Manager) persist() error {
	if m.store == nil {
		return nil
	}
	return m.store.Save(m.List())
}

func (m *Manager) persistOrders() error {
	if m.orderStore == nil {
		return nil
	}
	return m.orderStore.SaveOrders(m.ListSyncedOrders())
}

func (m *Manager) persistPositions() error {
	if m.positionStore == nil {
		return nil
	}
	return m.positionStore.SavePositions(m.ListSyncedPositions())
}

func (m *Manager) setEntryPriceIfMissing(id string, entryPrice float64) error {
	if entryPrice <= 0 {
		return nil
	}

	m.mu.Lock()
	trade, ok := m.trades[id]
	if !ok {
		m.mu.Unlock()
		return notFoundError("trade_not_found", "trade not found")
	}
	if trade.EntryPrice == 0 {
		trade.EntryPrice = entryPrice
		trade.UpdatedAt = time.Now().UTC()
	}
	m.mu.Unlock()
	return m.persist()
}

func (m *Manager) updateEntryStatus(id string, status string) error {
	if status == "" {
		return nil
	}

	m.mu.Lock()
	trade, ok := m.trades[id]
	if !ok {
		m.mu.Unlock()
		return notFoundError("trade_not_found", "trade not found")
	}
	if trade.EntryStatus == status {
		m.mu.Unlock()
		return nil
	}
	trade.EntryStatus = status
	trade.UpdatedAt = time.Now().UTC()
	m.mu.Unlock()
	return m.persist()
}

func (m *Manager) clearPendingProtection(id string) error {
	m.mu.Lock()
	trade, ok := m.trades[id]
	if !ok {
		m.mu.Unlock()
		return notFoundError("trade_not_found", "trade not found")
	}
	trade.PendingProtection = nil
	trade.UpdatedAt = time.Now().UTC()
	m.mu.Unlock()
	return m.persist()
}

func (m *Manager) updateExitOrderStatuses(id, stopStatus, targetStatus string) error {
	m.mu.Lock()
	trade, ok := m.trades[id]
	if !ok {
		m.mu.Unlock()
		return notFoundError("trade_not_found", "trade not found")
	}
	changed := false
	if stopStatus != "" && trade.StopOrderStatus != stopStatus {
		trade.StopOrderStatus = stopStatus
		changed = true
	}
	if targetStatus != "" && trade.TargetOrderStatus != targetStatus {
		trade.TargetOrderStatus = targetStatus
		changed = true
	}
	if changed {
		trade.UpdatedAt = time.Now().UTC()
	}
	m.mu.Unlock()
	if !changed {
		return nil
	}
	return m.persist()
}

func (m *Manager) reconcileOpenExitOrderDetails(ctx context.Context, id string, stopDetails, targetDetails *OrderDetails) error {
	m.mu.Lock()
	trade, ok := m.trades[id]
	if !ok {
		m.mu.Unlock()
		return notFoundError("trade_not_found", "trade not found")
	}

	changed := false
	if stopDetails != nil {
		if trade.StopOrderStatus != stopDetails.Status {
			trade.StopOrderStatus = stopDetails.Status
			changed = true
		}
		if isTerminalOrderStatus(stopDetails.Status) {
			trade.StopOrderID = ""
			trade.StopLoss = nil
			changed = true
		} else if trade.StopLoss != nil {
			if stopDetails.TriggerPrice > 0 && trade.StopLoss.TriggerPrice != stopDetails.TriggerPrice {
				trade.StopLoss.TriggerPrice = stopDetails.TriggerPrice
				changed = true
			}
			if stopDetails.Price > 0 && trade.StopLoss.LimitPrice != stopDetails.Price {
				trade.StopLoss.LimitPrice = stopDetails.Price
				changed = true
			}
		}
	}
	if targetDetails != nil {
		if trade.TargetOrderStatus != targetDetails.Status {
			trade.TargetOrderStatus = targetDetails.Status
			changed = true
		}
		if isTerminalOrderStatus(targetDetails.Status) {
			trade.TargetOrderID = ""
			trade.Target = nil
			changed = true
		} else if trade.Target != nil && targetDetails.Price > 0 && trade.Target.Price != targetDetails.Price {
			trade.Target.Price = targetDetails.Price
			changed = true
		}
	}

	if changed {
		trade.UpdatedAt = time.Now().UTC()
	}
	m.mu.Unlock()
	if !changed {
		return nil
	}
	if err := m.persist(); err != nil {
		return err
	}
	m.logger.Info("external_exit_order_reconciled",
		"request_id", observability.RequestID(ctx),
		"trade_id", id,
		"stop_order_status", orderStatusFromDetails(stopDetails),
		"target_order_status", orderStatusFromDetails(targetDetails),
	)
	return nil
}

func (m *Manager) closeTrade(ctx context.Context, id, exitReason, stopStatus, targetStatus string) error {
	m.mu.Lock()
	trade, ok := m.trades[id]
	if !ok {
		m.mu.Unlock()
		return notFoundError("trade_not_found", "trade not found")
	}
	now := time.Now().UTC()
	trade.TradeStatus = TradeStatusClosed
	trade.ExitReason = exitReason
	trade.ClosedAt = &now
	if stopStatus != "" {
		trade.StopOrderStatus = stopStatus
	}
	if targetStatus != "" {
		trade.TargetOrderStatus = targetStatus
	}
	trade.PendingProtection = nil
	trade.PendingStopLoss = nil
	trade.PendingTarget = nil
	trade.UpdatedAt = now
	result := cloneTrade(trade)
	m.mu.Unlock()
	if err := m.persist(); err != nil {
		return err
	}
	m.logTradeEvent(ctx, "trade_closed", result)
	return nil
}

func (m *Manager) setPendingStopLoss(id string, req StopLossRequest) (*ManagedTrade, error) {
	m.mu.Lock()
	trade, ok := m.trades[id]
	if !ok {
		m.mu.Unlock()
		return nil, notFoundError("trade_not_found", "trade not found")
	}
	trade.PendingStopLoss = &StopLoss{
		TriggerPrice: req.TriggerPrice,
		LimitPrice:   req.LimitPrice,
		TrailBy:      req.TrailBy,
	}
	trade.UpdatedAt = time.Now().UTC()
	result := cloneTrade(trade)
	m.mu.Unlock()
	if err := m.persist(); err != nil {
		return nil, err
	}
	return result, nil
}

func (m *Manager) setPendingTarget(id string, req TargetRequest) (*ManagedTrade, error) {
	m.mu.Lock()
	trade, ok := m.trades[id]
	if !ok {
		m.mu.Unlock()
		return nil, notFoundError("trade_not_found", "trade not found")
	}
	trade.PendingTarget = &Target{Price: req.Price}
	trade.UpdatedAt = time.Now().UTC()
	result := cloneTrade(trade)
	m.mu.Unlock()
	if err := m.persist(); err != nil {
		return nil, err
	}
	return result, nil
}

func (m *Manager) findByEntryOrderIDLocked(entryOrderID string) *ManagedTrade {
	for _, trade := range m.trades {
		if trade.EntryOrderID == entryOrderID {
			return trade
		}
	}
	return nil
}

func (m *Manager) activeOppositeSideGroup(exchange, symbol, product, side string) *PositionGroup {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, group := range m.positionGroupsLocked() {
		if !samePositionGroup(group.Exchange, group.TradingSymbol, group.Product, exchange, symbol, product) {
			continue
		}
		if group.Side != "" && group.Side != side {
			return group
		}
	}
	return nil
}

func (m *Manager) positionGroupsLocked() []*PositionGroup {
	groupsByID := make(map[string]*PositionGroup)
	for _, trade := range m.trades {
		if isTradeClosed(trade) {
			continue
		}
		groupID := positionGroupID(trade.Exchange, trade.TradingSymbol, trade.Product)
		group := groupsByID[groupID]
		if group == nil {
			group = &PositionGroup{
				ID:               groupID,
				Exchange:         strings.ToUpper(trade.Exchange),
				TradingSymbol:    strings.ToUpper(trade.TradingSymbol),
				Product:          strings.ToUpper(trade.Product),
				Side:             strings.ToUpper(trade.Side),
				TradeStatus:      TradeStatusOpen,
				CreationSource:   CreationSourceLocalSystem,
				ManagementStatus: ManagementStatusManaged,
				CreatedAt:        trade.CreatedAt,
				UpdatedAt:        trade.UpdatedAt,
			}
			groupsByID[groupID] = group
		}
		group.TradeIDs = append(group.TradeIDs, trade.ID)
		group.LocalQuantity += trade.Quantity
		group.Quantity = group.LocalQuantity
		if trade.EntryPrice > 0 {
			group.AverageEntryPrice += trade.EntryPrice * float64(trade.Quantity)
		}
		if trade.CreatedAt.Before(group.CreatedAt) {
			group.CreatedAt = trade.CreatedAt
		}
		if trade.UpdatedAt.After(group.UpdatedAt) {
			group.UpdatedAt = trade.UpdatedAt
		}
	}

	for _, position := range m.positions {
		if position.Quantity == 0 {
			continue
		}
		groupID := positionGroupID(position.Exchange, position.TradingSymbol, position.Product)
		group := groupsByID[groupID]
		brokerQuantity := absInt(position.Quantity)
		brokerSide := sideFromQuantity(position.Quantity)
		if group == nil {
			group = &PositionGroup{
				ID:               groupID,
				Exchange:         strings.ToUpper(position.Exchange),
				TradingSymbol:    strings.ToUpper(position.TradingSymbol),
				Product:          strings.ToUpper(position.Product),
				Side:             brokerSide,
				Quantity:         brokerQuantity,
				BrokerQuantity:   brokerQuantity,
				TradeStatus:      TradeStatusOpen,
				CreationSource:   CreationSourceKiteApp,
				ManagementStatus: ManagementStatusUnmanaged,
				CreatedAt:        position.SyncedAt,
				UpdatedAt:        position.SyncedAt,
			}
			groupsByID[groupID] = group
			continue
		}
		group.BrokerQuantity = brokerQuantity
		group.Quantity = brokerQuantity
		if brokerSide != group.Side {
			group.Warnings = appendUnique(group.Warnings, WarningPositionQuantityMismatch)
			group.ManagementStatus = ManagementStatusPartiallyManaged
		} else if brokerQuantity < group.LocalQuantity {
			group.Warnings = appendUnique(group.Warnings, WarningPartialExternalExit)
			group.ManagementStatus = ManagementStatusPartiallyManaged
		} else if brokerQuantity > group.LocalQuantity {
			group.Warnings = appendUnique(group.Warnings, WarningPositionQuantityMismatch)
			group.ManagementStatus = ManagementStatusPartiallyManaged
		}
		if position.SyncedAt.After(group.UpdatedAt) {
			group.UpdatedAt = position.SyncedAt
		}
	}

	for fromKey, toKey := range m.productConversions {
		fromGroup := groupsByID[fromKey]
		toGroup := groupsByID[toKey]
		if fromGroup != nil && toGroup != nil {
			fromGroup.Warnings = appendUnique(fromGroup.Warnings, WarningProductConversionDetected)
			fromGroup.ConvertedToProduct = toGroup.Product
			fromGroup.ManagementStatus = ManagementStatusPartiallyManaged
			toGroup.Warnings = appendUnique(toGroup.Warnings, WarningProductConversionDetected)
			toGroup.ConvertedFromProduct = fromGroup.Product
		}
	}

	groups := make([]*PositionGroup, 0, len(groupsByID))
	for _, group := range groupsByID {
		if m.positionsSynced && group.CreationSource == CreationSourceLocalSystem && group.BrokerQuantity == 0 {
			group.Warnings = appendUnique(group.Warnings, WarningPositionMissingFromBroker)
			group.ManagementStatus = ManagementStatusPartiallyManaged
		}
		sort.Strings(group.TradeIDs)
		if group.LocalQuantity > 0 && group.AverageEntryPrice > 0 {
			group.AverageEntryPrice = group.AverageEntryPrice / float64(group.LocalQuantity)
		}
		groups = append(groups, group)
	}
	addCrossProductExposureWarnings(groups)
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].ID < groups[j].ID
	})
	return groups
}

func addCrossProductExposureWarnings(groups []*PositionGroup) {
	for i, group := range groups {
		for j, other := range groups {
			if i == j {
				continue
			}
			if group.Exchange != other.Exchange || group.TradingSymbol != other.TradingSymbol {
				continue
			}
			if group.Product == other.Product || group.Side == other.Side {
				continue
			}
			group.Warnings = appendUnique(group.Warnings, WarningOppositeExposureAcrossProducts)
		}
	}
}

func positionGroupID(exchange, symbol, product string) string {
	return strings.ToUpper(exchange) + ":" + strings.ToUpper(symbol) + ":" + strings.ToUpper(product)
}

func positionKey(exchange, symbol, product string) string {
	return strings.ToUpper(exchange) + ":" + strings.ToUpper(symbol) + ":" + strings.ToUpper(product)
}

func productFromPositionKey(key string) string {
	parts := strings.Split(key, ":")
	if len(parts) != 3 {
		return ""
	}
	return parts[2]
}

func countPositionChanges(previous, next map[string]*KitePosition, result *SyncResult) {
	for key, position := range next {
		old, ok := previous[key]
		if !ok {
			result.PositionsAdded++
			continue
		}
		if old.Quantity != position.Quantity {
			result.PositionsChanged++
		}
	}
	for key := range previous {
		if _, ok := next[key]; !ok {
			result.PositionsRemoved++
		}
	}
}

func detectProductConversions(previous, next map[string]*KitePosition) map[string]string {
	conversions := make(map[string]string)
	for oldKey, oldPosition := range previous {
		if oldPosition == nil || oldPosition.Quantity == 0 {
			continue
		}
		if current := next[oldKey]; current != nil && current.Quantity != 0 {
			continue
		}

		var candidateKey string
		for newKey, newPosition := range next {
			if newPosition == nil || newPosition.Quantity == 0 || newKey == oldKey {
				continue
			}
			if !strings.EqualFold(oldPosition.Exchange, newPosition.Exchange) || !strings.EqualFold(oldPosition.TradingSymbol, newPosition.TradingSymbol) {
				continue
			}
			if oldPosition.Quantity != newPosition.Quantity {
				continue
			}
			if candidateKey != "" {
				candidateKey = ""
				break
			}
			candidateKey = newKey
		}
		if candidateKey != "" {
			conversions[oldKey] = candidateKey
		}
	}
	return conversions
}

func retainProductConversions(existing map[string]string, positions map[string]*KitePosition) map[string]string {
	retained := make(map[string]string)
	for fromKey, toKey := range existing {
		if position := positions[toKey]; position != nil && position.Quantity != 0 {
			retained[fromKey] = toKey
		}
	}
	return retained
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func sideFromQuantity(quantity int) string {
	if quantity < 0 {
		return "SELL"
	}
	return "BUY"
}

func sameSignedQuantity(left, right int) bool {
	return (left > 0 && right > 0) || (left < 0 && right < 0)
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func creationSourceFromTag(tag string) string {
	if strings.EqualFold(tag, LocalSystemOrderTag) {
		return CreationSourceLocalSystem
	}
	if tag == "" {
		return CreationSourceKiteApp
	}
	return CreationSourceUnknown
}

func samePositionGroup(leftExchange, leftSymbol, leftProduct, rightExchange, rightSymbol, rightProduct string) bool {
	return positionGroupID(leftExchange, leftSymbol, leftProduct) == positionGroupID(rightExchange, rightSymbol, rightProduct)
}

func (m *Manager) logTradeEvent(ctx context.Context, event string, trade *ManagedTrade) {
	if m.logger == nil || trade == nil {
		return
	}
	m.logger.InfoContext(ctx, event,
		"request_id", observability.RequestID(ctx),
		"trade_id", trade.ID,
		"exchange", trade.Exchange,
		"tradingsymbol", trade.TradingSymbol,
		"side", trade.Side,
		"quantity", trade.Quantity,
		"product", trade.Product,
		"entry_order_id", trade.EntryOrderID,
		"stop_order_id", trade.StopOrderID,
		"target_order_id", trade.TargetOrderID,
		"trade_status", trade.TradeStatus,
		"exit_reason", trade.ExitReason,
	)
}

func cloneTrade(trade *ManagedTrade) *ManagedTrade {
	if trade == nil {
		return nil
	}
	copy := *trade
	if trade.StopLoss != nil {
		sl := *trade.StopLoss
		copy.StopLoss = &sl
	}
	if trade.Target != nil {
		target := *trade.Target
		copy.Target = &target
	}
	if trade.PendingProtection != nil {
		protection := *trade.PendingProtection
		copy.PendingProtection = &protection
	}
	if trade.PendingStopLoss != nil {
		stopLoss := *trade.PendingStopLoss
		copy.PendingStopLoss = &stopLoss
	}
	if trade.PendingTarget != nil {
		target := *trade.PendingTarget
		copy.PendingTarget = &target
	}
	if trade.ClosedAt != nil {
		closedAt := *trade.ClosedAt
		copy.ClosedAt = &closedAt
	}
	return &copy
}

func cloneKiteOrder(order *KiteOrder) *KiteOrder {
	if order == nil {
		return nil
	}
	copy := *order
	return &copy
}

func cloneKitePosition(position *KitePosition) *KitePosition {
	if position == nil {
		return nil
	}
	copy := *position
	return &copy
}

func opposite(side string) string {
	if side == "BUY" {
		return "SELL"
	}
	return "BUY"
}

func validateStopLimit(exitSide string, triggerPrice, limitPrice float64) error {
	if exitSide == "BUY" && limitPrice < triggerPrice {
		return validationError("invalid_buy_stop_limit", "for BUY stop-loss orders, limit_price must be equal to or greater than trigger_price")
	}
	if exitSide == "SELL" && limitPrice > triggerPrice {
		return validationError("invalid_sell_stop_limit", "for SELL stop-loss orders, limit_price must be equal to or less than trigger_price")
	}
	return nil
}

func validateStopAgainstEntry(trade *ManagedTrade, triggerPrice float64) error {
	if trade.EntryPrice == 0 {
		return nil
	}
	if trade.Side == "BUY" && triggerPrice >= trade.EntryPrice {
		return validationError("invalid_buy_stop_loss", fmt.Sprintf("for BUY trades, stop-loss trigger_price must be below entry_price %.2f", trade.EntryPrice))
	}
	if trade.Side == "SELL" && triggerPrice <= trade.EntryPrice {
		return validationError("invalid_sell_stop_loss", fmt.Sprintf("for SELL trades, stop-loss trigger_price must be above entry_price %.2f", trade.EntryPrice))
	}
	return nil
}

func validateTargetAgainstEntry(trade *ManagedTrade, targetPrice float64) error {
	if trade.EntryPrice == 0 {
		return nil
	}
	if trade.Side == "BUY" && targetPrice <= trade.EntryPrice {
		return validationError("invalid_buy_target", fmt.Sprintf("for BUY trades, target price must be above entry_price %.2f", trade.EntryPrice))
	}
	if trade.Side == "SELL" && targetPrice >= trade.EntryPrice {
		return validationError("invalid_sell_target", fmt.Sprintf("for SELL trades, target price must be below entry_price %.2f", trade.EntryPrice))
	}
	return nil
}

func validateAutoProtectionRequest(exchange, side string, orderPrice float64, req AutoProtectionRequest) error {
	if req.StopLossPoints < 0 {
		return validationError("negative_stop_loss_points", "stop_loss_points cannot be negative")
	}
	if req.TargetPoints < 0 {
		return validationError("negative_target_points", "target_points cannot be negative")
	}
	if req.TrailBy < 0 {
		return validationError("negative_trail_by", "trail_by cannot be negative")
	}
	if req.SLLimitOffset < 0 {
		return validationError("negative_sl_limit_offset", "sl_limit_offset cannot be negative")
	}

	referencePrice := req.ReferencePrice
	if referencePrice == 0 {
		referencePrice = orderPrice
	}
	if referencePrice == 0 {
		return nil
	}
	if req.StopLossPoints > 0 {
		triggerPrice, limitPrice := stopPrices(exchange, side, referencePrice, req.StopLossPoints, req.SLLimitOffset)
		if triggerPrice <= 0 || limitPrice <= 0 {
			return validationError("invalid_computed_stop_loss", "computed stop-loss trigger_price and limit_price must be positive")
		}
	}
	if req.TargetPoints > 0 && targetPrice(side, referencePrice, req.TargetPoints) <= 0 {
		return validationError("invalid_computed_target", "computed target price must be positive")
	}
	return nil
}

func initialEntryPrice(req CreateTradeRequest) float64 {
	if req.Protection != nil && req.Protection.ReferencePrice > 0 {
		return req.Protection.ReferencePrice
	}
	return req.Price
}

func initialEntryStatus(req CreateTradeRequest) string {
	if strings.EqualFold(valueOr(req.OrderType, "MARKET"), "MARKET") {
		return "COMPLETE"
	}
	return "OPEN"
}

func shouldDeferProtection(req CreateTradeRequest) bool {
	return !strings.EqualFold(valueOr(req.OrderType, "MARKET"), "MARKET")
}

func shouldDeferRiskOrder(trade *ManagedTrade) bool {
	return !strings.EqualFold(trade.EntryStatus, "COMPLETE")
}

func ensureTradeOpen(trade *ManagedTrade) error {
	if isTradeClosed(trade) {
		return closedError(fmt.Sprintf("trade is closed: %s", trade.ExitReason))
	}
	return nil
}

func isTradeClosed(trade *ManagedTrade) bool {
	return strings.EqualFold(trade.TradeStatus, TradeStatusClosed)
}

func isTerminalOrderStatus(status string) bool {
	return strings.EqualFold(status, OrderStatusComplete) ||
		strings.EqualFold(status, OrderStatusCancelled) ||
		strings.EqualFold(status, OrderStatusRejected)
}

func pendingProtection(req AutoProtectionRequest) *PendingProtection {
	return &PendingProtection{
		ReferencePrice: req.ReferencePrice,
		StopLossPoints: req.StopLossPoints,
		TargetPoints:   req.TargetPoints,
		TrailBy:        req.TrailBy,
		SLLimitOffset:  req.SLLimitOffset,
	}
}

func stopPrices(exchange, side string, referencePrice, points, limitOffset float64) (float64, float64) {
	if limitOffset <= 0 {
		limitOffset = defaultSLLimitOffset(exchange)
	}
	if side == "BUY" {
		triggerPrice := referencePrice - points
		return triggerPrice, triggerPrice - limitOffset
	}

	triggerPrice := referencePrice + points
	return triggerPrice, triggerPrice + limitOffset
}

func targetPrice(side string, referencePrice, points float64) float64 {
	if side == "BUY" {
		return referencePrice + points
	}
	return referencePrice - points
}

func money(value float64) string {
	return strconv.FormatFloat(value, 'f', 2, 64)
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minNonZero(a, b float64) float64 {
	if a == 0 || b < a {
		return b
	}
	return a
}

func intPtr(value int) *int {
	return &value
}

func positionQuantity(positions []Position, exchange, symbol, product string) int {
	for _, position := range positions {
		if strings.EqualFold(position.Exchange, exchange) &&
			strings.EqualFold(position.TradingSymbol, symbol) &&
			strings.EqualFold(position.Product, product) {
			return position.Quantity
		}
	}
	return 0
}

func orderStatusFromDetails(details *OrderDetails) string {
	if details == nil {
		return ""
	}
	return details.Status
}
