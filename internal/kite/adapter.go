package kite

import (
	"context"
	"strings"
	"time"

	"trading-system/internal/trading"
)

type Adapter struct {
	client *Client
}

func NewAdapter(client *Client) *Adapter {
	return &Adapter{client: client}
}

func (a *Adapter) PlaceOrder(ctx context.Context, order trading.Order) (string, error) {
	return a.client.PlaceOrder(ctx, OrderRequest{
		Exchange:         order.Exchange,
		TradingSymbol:    order.TradingSymbol,
		TransactionType:  order.TransactionType,
		Quantity:         order.Quantity,
		Product:          order.Product,
		OrderType:        order.OrderType,
		Variety:          order.Variety,
		Validity:         order.Validity,
		Price:            order.Price,
		TriggerPrice:     order.TriggerPrice,
		MarketProtection: order.MarketProtection,
		Tag:              order.Tag,
	})
}

func (a *Adapter) ModifyOrder(ctx context.Context, variety, orderID string, fields map[string]string) error {
	return a.client.ModifyOrder(ctx, variety, orderID, fields)
}

func (a *Adapter) CancelOrder(ctx context.Context, variety, orderID string) error {
	return a.client.CancelOrder(ctx, variety, orderID)
}

func (a *Adapter) OrderStatus(ctx context.Context, _ string, orderID string) (string, error) {
	return a.client.OrderStatus(ctx, orderID)
}

func (a *Adapter) OrderDetails(ctx context.Context, _ string, orderID string) (*trading.OrderDetails, error) {
	details, err := a.client.OrderDetails(ctx, orderID)
	if err != nil {
		return nil, err
	}
	return &trading.OrderDetails{
		OrderID:         details.OrderID,
		Status:          details.Status,
		Price:           details.Price,
		TriggerPrice:    details.TriggerPrice,
		FilledQuantity:  details.FilledQuantity,
		PendingQuantity: details.PendingQuantity,
	}, nil
}

func (a *Adapter) Orders(ctx context.Context) ([]trading.KiteOrder, error) {
	orders, err := a.client.Orders(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]trading.KiteOrder, 0, len(orders))
	for _, order := range orders {
		out = append(out, trading.KiteOrder{
			OrderID:         order.OrderID,
			Exchange:        order.Exchange,
			TradingSymbol:   order.TradingSymbol,
			TransactionType: order.TransactionType,
			Quantity:        order.Quantity,
			FilledQuantity:  order.FilledQuantity,
			PendingQuantity: order.PendingQuantity,
			Product:         order.Product,
			OrderType:       order.OrderType,
			Status:          order.Status,
			Price:           order.Price,
			TriggerPrice:    order.TriggerPrice,
			AveragePrice:    order.AveragePrice,
			Tag:             order.Tag,
			OrderTimestamp:  kiteOrderTime(order),
		})
	}
	return out, nil
}

func kiteOrderTime(order OrderStatusResponse) time.Time {
	for _, raw := range []string{order.OrderTimestamp, order.ExchangeUpdateTimestamp, order.ExchangeTimestamp} {
		if parsed, ok := parseKiteTimestamp(raw); ok {
			return parsed
		}
	}
	return time.Time{}
}

func parseKiteTimestamp(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		time.RFC3339,
	} {
		parsed, err := time.ParseInLocation(layout, raw, time.Local)
		if err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func (a *Adapter) Positions(ctx context.Context) ([]trading.Position, error) {
	positions, err := a.client.Positions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]trading.Position, 0, len(positions))
	for _, position := range positions {
		out = append(out, trading.Position{
			Exchange:      position.Exchange,
			TradingSymbol: position.TradingSymbol,
			Product:       position.Product,
			Quantity:      position.Quantity,
		})
	}
	return out, nil
}

func (a *Adapter) LTP(ctx context.Context, exchange, symbol string) (float64, error) {
	return a.client.LTP(ctx, exchange, symbol)
}
