package trading

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	broker Broker
	store  Store
	mu     sync.RWMutex
	trades map[string]*ManagedTrade
}

func NewManager(broker Broker) *Manager {
	manager, _ := NewManagerWithStore(broker, nil)
	return manager
}

func NewManagerWithStore(broker Broker, store Store) (*Manager, error) {
	manager := &Manager{
		broker: broker,
		store:  store,
		trades: make(map[string]*ManagedTrade),
	}
	if store == nil {
		return manager, nil
	}

	trades, err := store.Load()
	if err != nil {
		return nil, err
	}
	for _, trade := range trades {
		if trade == nil || trade.ID == "" {
			continue
		}
		manager.trades[trade.ID] = cloneTrade(trade)
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
		return nil, errors.New("side must be BUY or SELL")
	}
	if req.Quantity <= 0 {
		return nil, errors.New("quantity must be positive")
	}
	if req.Protection != nil {
		if err := validateAutoProtectionRequest(req.Exchange, side, req.Price, *req.Protection); err != nil {
			return nil, err
		}
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
	})
	if err != nil {
		return nil, err
	}

	trade := &ManagedTrade{
		ID:            fmt.Sprintf("%s-%d", strings.ToLower(req.TradingSymbol), now.UnixNano()),
		Exchange:      req.Exchange,
		TradingSymbol: req.TradingSymbol,
		Side:          side,
		Quantity:      req.Quantity,
		Product:       valueOr(req.Product, "MIS"),
		EntryPrice:    initialEntryPrice(req),
		EntryOrderID:  orderID,
		EntryStatus:   initialEntryStatus(req),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if req.Protection != nil && shouldDeferProtection(req) {
		trade.PendingProtection = pendingProtection(*req.Protection)
	}

	m.mu.Lock()
	if existing, ok := m.trades[trade.ID]; ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("trade id already exists: %s", existing.ID)
	}
	if existing := m.findByEntryOrderIDLocked(trade.EntryOrderID); existing != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("entry_order_id already exists on trade %s", existing.ID)
	}
	m.trades[trade.ID] = trade
	m.mu.Unlock()
	if err := m.persist(); err != nil {
		return nil, err
	}

	if req.Protection != nil && !shouldDeferProtection(req) {
		return m.applyAutoProtection(ctx, trade.ID, req.Price, *req.Protection)
	}

	return cloneTrade(trade), nil
}

func (m *Manager) Import(req ImportTradeRequest) (*ManagedTrade, error) {
	side := strings.ToUpper(req.Side)
	if side != "BUY" && side != "SELL" {
		return nil, errors.New("side must be BUY or SELL")
	}
	if req.Quantity <= 0 {
		return nil, errors.New("quantity must be positive")
	}
	if req.EntryOrderID == "" {
		return nil, errors.New("entry_order_id is required")
	}

	now := time.Now().UTC()
	id := req.ID
	if id == "" {
		id = fmt.Sprintf("%s-%d", strings.ToLower(req.TradingSymbol), now.UnixNano())
	}
	trade := &ManagedTrade{
		ID:            id,
		Exchange:      req.Exchange,
		TradingSymbol: req.TradingSymbol,
		Side:          side,
		Quantity:      req.Quantity,
		Product:       valueOr(req.Product, "MIS"),
		EntryPrice:    req.EntryPrice,
		EntryOrderID:  req.EntryOrderID,
		EntryStatus:   "COMPLETE",
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	m.mu.Lock()
	if existing, ok := m.trades[trade.ID]; ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("trade id already exists: %s", existing.ID)
	}
	if existing := m.findByEntryOrderIDLocked(trade.EntryOrderID); existing != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("entry_order_id already exists on trade %s", existing.ID)
	}
	m.trades[trade.ID] = trade
	m.mu.Unlock()
	if err := m.persist(); err != nil {
		return nil, err
	}

	return cloneTrade(trade), nil
}

type StopLossRequest struct {
	TriggerPrice float64 `json:"trigger_price"`
	LimitPrice   float64 `json:"limit_price"`
	TrailBy      float64 `json:"trail_by,omitempty"`
}

func (m *Manager) AddStopLoss(ctx context.Context, id string, req StopLossRequest) (*ManagedTrade, error) {
	if req.TriggerPrice <= 0 || req.LimitPrice <= 0 {
		return nil, errors.New("trigger_price and limit_price are required")
	}

	trade, err := m.get(id)
	if err != nil {
		return nil, err
	}

	exitSide := opposite(trade.Side)
	if err := validateStopAgainstEntry(trade, req.TriggerPrice); err != nil {
		return nil, err
	}
	if err := validateStopLimit(exitSide, req.TriggerPrice, req.LimitPrice); err != nil {
		return nil, err
	}
	orderID := trade.StopOrderID
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
		})
		if err != nil {
			return nil, err
		}
	} else {
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
	current.StopLoss = &StopLoss{
		TriggerPrice: req.TriggerPrice,
		LimitPrice:   req.LimitPrice,
		TrailBy:      req.TrailBy,
	}
	current.UpdatedAt = time.Now().UTC()
	result := cloneTrade(current)
	m.mu.Unlock()
	if err := m.persist(); err != nil {
		return nil, err
	}
	return result, nil
}

type TargetRequest struct {
	Price float64 `json:"price"`
}

func (m *Manager) AddTarget(ctx context.Context, id string, req TargetRequest) (*ManagedTrade, error) {
	if req.Price <= 0 {
		return nil, errors.New("price is required")
	}

	trade, err := m.get(id)
	if err != nil {
		return nil, err
	}
	if err := validateTargetAgainstEntry(trade, req.Price); err != nil {
		return nil, err
	}

	orderID := trade.TargetOrderID
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
		})
		if err != nil {
			return nil, err
		}
	} else {
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
	current.Target = &Target{Price: req.Price}
	current.UpdatedAt = time.Now().UTC()
	result := cloneTrade(current)
	m.mu.Unlock()
	if err := m.persist(); err != nil {
		return nil, err
	}
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
	current.StopLoss = nil
	current.UpdatedAt = time.Now().UTC()
	result := cloneTrade(current)
	m.mu.Unlock()
	if err := m.persist(); err != nil {
		return nil, err
	}
	return result, nil
}

func (m *Manager) RemoveTarget(ctx context.Context, id string) (*ManagedTrade, error) {
	trade, err := m.get(id)
	if err != nil {
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
	current.Target = nil
	current.UpdatedAt = time.Now().UTC()
	result := cloneTrade(current)
	m.mu.Unlock()
	if err := m.persist(); err != nil {
		return nil, err
	}
	return result, nil
}

func (m *Manager) Exit(ctx context.Context, id string) (*ManagedTrade, error) {
	trade, err := m.get(id)
	if err != nil {
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
	})
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	current := m.trades[id]
	current.ExitOrderID = exitOrderID
	current.StopOrderID = ""
	current.TargetOrderID = ""
	current.StopLoss = nil
	current.Target = nil
	current.UpdatedAt = time.Now().UTC()
	result := cloneTrade(current)
	m.mu.Unlock()
	if err := m.persist(); err != nil {
		return nil, err
	}
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

func (m *Manager) TrailStops(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.trailOnce(ctx)
		}
	}
}

func (m *Manager) trailOnce(ctx context.Context) {
	for _, trade := range m.List() {
		m.tryApplyPendingProtection(ctx, trade)

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

func (m *Manager) tryApplyPendingProtection(ctx context.Context, trade *ManagedTrade) {
	if trade.PendingProtection == nil || trade.EntryOrderID == "" {
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
		return nil, errors.New("trade not found")
	}
	return cloneTrade(trade), nil
}

func (m *Manager) persist() error {
	if m.store == nil {
		return nil
	}
	return m.store.Save(m.List())
}

func (m *Manager) setEntryPriceIfMissing(id string, entryPrice float64) error {
	if entryPrice <= 0 {
		return nil
	}

	m.mu.Lock()
	trade, ok := m.trades[id]
	if !ok {
		m.mu.Unlock()
		return errors.New("trade not found")
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
		return errors.New("trade not found")
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
		return errors.New("trade not found")
	}
	trade.PendingProtection = nil
	trade.UpdatedAt = time.Now().UTC()
	m.mu.Unlock()
	return m.persist()
}

func (m *Manager) findByEntryOrderIDLocked(entryOrderID string) *ManagedTrade {
	for _, trade := range m.trades {
		if trade.EntryOrderID == entryOrderID {
			return trade
		}
	}
	return nil
}

func cloneTrade(trade *ManagedTrade) *ManagedTrade {
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
		return errors.New("for BUY stop-loss orders, limit_price must be equal to or greater than trigger_price")
	}
	if exitSide == "SELL" && limitPrice > triggerPrice {
		return errors.New("for SELL stop-loss orders, limit_price must be equal to or less than trigger_price")
	}
	return nil
}

func validateStopAgainstEntry(trade *ManagedTrade, triggerPrice float64) error {
	if trade.EntryPrice == 0 {
		return nil
	}
	if trade.Side == "BUY" && triggerPrice >= trade.EntryPrice {
		return fmt.Errorf("for BUY trades, stop-loss trigger_price must be below entry_price %.2f", trade.EntryPrice)
	}
	if trade.Side == "SELL" && triggerPrice <= trade.EntryPrice {
		return fmt.Errorf("for SELL trades, stop-loss trigger_price must be above entry_price %.2f", trade.EntryPrice)
	}
	return nil
}

func validateTargetAgainstEntry(trade *ManagedTrade, targetPrice float64) error {
	if trade.EntryPrice == 0 {
		return nil
	}
	if trade.Side == "BUY" && targetPrice <= trade.EntryPrice {
		return fmt.Errorf("for BUY trades, target price must be above entry_price %.2f", trade.EntryPrice)
	}
	if trade.Side == "SELL" && targetPrice >= trade.EntryPrice {
		return fmt.Errorf("for SELL trades, target price must be below entry_price %.2f", trade.EntryPrice)
	}
	return nil
}

func validateAutoProtectionRequest(exchange, side string, orderPrice float64, req AutoProtectionRequest) error {
	if req.StopLossPoints < 0 {
		return errors.New("stop_loss_points cannot be negative")
	}
	if req.TargetPoints < 0 {
		return errors.New("target_points cannot be negative")
	}
	if req.TrailBy < 0 {
		return errors.New("trail_by cannot be negative")
	}
	if req.SLLimitOffset < 0 {
		return errors.New("sl_limit_offset cannot be negative")
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
			return errors.New("computed stop-loss trigger_price and limit_price must be positive")
		}
	}
	if req.TargetPoints > 0 && targetPrice(side, referencePrice, req.TargetPoints) <= 0 {
		return errors.New("computed target price must be positive")
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
