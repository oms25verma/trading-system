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
	mu     sync.RWMutex
	trades map[string]*ManagedTrade
}

func NewManager(broker Broker) *Manager {
	return &Manager{
		broker: broker,
		trades: make(map[string]*ManagedTrade),
	}
}

type CreateTradeRequest struct {
	Exchange         string  `json:"exchange"`
	TradingSymbol    string  `json:"tradingsymbol"`
	Side             string  `json:"side"`
	Quantity         int     `json:"quantity"`
	Product          string  `json:"product"`
	OrderType        string  `json:"order_type"`
	Price            float64 `json:"price,omitempty"`
	MarketProtection *int    `json:"market_protection,omitempty"`
}

func (m *Manager) Enter(ctx context.Context, req CreateTradeRequest) (*ManagedTrade, error) {
	side := strings.ToUpper(req.Side)
	if side != "BUY" && side != "SELL" {
		return nil, errors.New("side must be BUY or SELL")
	}
	if req.Quantity <= 0 {
		return nil, errors.New("quantity must be positive")
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
		EntryOrderID:  orderID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	m.mu.Lock()
	m.trades[trade.ID] = trade
	m.mu.Unlock()

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
	if err := validateStopLimit(exitSide, req.TriggerPrice, req.LimitPrice); err != nil {
		return nil, err
	}
	orderID, err := m.broker.PlaceOrder(ctx, Order{
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

	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.trades[id]
	current.StopOrderID = orderID
	current.StopLoss = &StopLoss{
		TriggerPrice: req.TriggerPrice,
		LimitPrice:   req.LimitPrice,
		TrailBy:      req.TrailBy,
	}
	current.UpdatedAt = time.Now().UTC()
	return cloneTrade(current), nil
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

	orderID, err := m.broker.PlaceOrder(ctx, Order{
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

	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.trades[id]
	current.TargetOrderID = orderID
	current.Target = &Target{Price: req.Price}
	current.UpdatedAt = time.Now().UTC()
	return cloneTrade(current), nil
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
	defer m.mu.Unlock()
	current := m.trades[id]
	current.StopOrderID = ""
	current.StopLoss = nil
	current.UpdatedAt = time.Now().UTC()
	return cloneTrade(current), nil
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
	defer m.mu.Unlock()
	current := m.trades[id]
	current.TargetOrderID = ""
	current.Target = nil
	current.UpdatedAt = time.Now().UTC()
	return cloneTrade(current), nil
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
	defer m.mu.Unlock()
	current := m.trades[id]
	current.ExitOrderID = exitOrderID
	current.StopOrderID = ""
	current.TargetOrderID = ""
	current.StopLoss = nil
	current.Target = nil
	current.UpdatedAt = time.Now().UTC()
	return cloneTrade(current), nil
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
		return candidate, candidate - 0.05, true
	}

	reference := minNonZero(sl.LowestLTP, ltp)
	candidate := reference + sl.TrailBy
	if candidate >= sl.TriggerPrice {
		return 0, 0, false
	}
	return candidate, candidate + 0.05, true
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
