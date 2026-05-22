package kite

import (
	"context"

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

func (a *Adapter) LTP(ctx context.Context, exchange, symbol string) (float64, error) {
	return a.client.LTP(ctx, exchange, symbol)
}
