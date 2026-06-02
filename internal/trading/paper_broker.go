package trading

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"
)

type PaperBroker struct {
	mu        sync.Mutex
	nextID    int
	ltp       map[string]float64
	orders    map[string]Order
	status    map[string]string
	positions []Position
}

func NewPaperBroker() *PaperBroker {
	return &PaperBroker{
		ltp:    make(map[string]float64),
		orders: make(map[string]Order),
		status: make(map[string]string),
	}
}

func (p *PaperBroker) PlaceOrder(_ context.Context, order Order) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.nextID++
	id := "paper-" + strconv.Itoa(p.nextID)
	p.orders[id] = order
	if order.OrderType == "MARKET" {
		p.status[id] = "COMPLETE"
	} else {
		p.status[id] = "OPEN"
	}
	if order.Price > 0 {
		p.ltp[key(order.Exchange, order.TradingSymbol)] = order.Price
	}
	if _, ok := p.ltp[key(order.Exchange, order.TradingSymbol)]; !ok {
		p.ltp[key(order.Exchange, order.TradingSymbol)] = 100
	}
	return id, nil
}

func (p *PaperBroker) ModifyOrder(_ context.Context, _ string, orderID string, fields map[string]string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	order, ok := p.orders[orderID]
	if !ok {
		return fmt.Errorf("paper order %s not found", orderID)
	}
	if price, ok := fields["price"]; ok {
		order.Price, _ = strconv.ParseFloat(price, 64)
	}
	if trigger, ok := fields["trigger_price"]; ok {
		order.TriggerPrice, _ = strconv.ParseFloat(trigger, 64)
	}
	if quantity, ok := fields["quantity"]; ok {
		order.Quantity, _ = strconv.Atoi(quantity)
	}
	p.orders[orderID] = order
	return nil
}

func (p *PaperBroker) OrderStatus(_ context.Context, _ string, orderID string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	status, ok := p.status[orderID]
	if !ok {
		return "", fmt.Errorf("paper order %s not found", orderID)
	}
	return status, nil
}

func (p *PaperBroker) OrderDetails(_ context.Context, _ string, orderID string) (*OrderDetails, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	order, ok := p.orders[orderID]
	if !ok {
		return nil, fmt.Errorf("paper order %s not found", orderID)
	}
	return &OrderDetails{
		OrderID:      orderID,
		Status:       p.status[orderID],
		Price:        order.Price,
		TriggerPrice: order.TriggerPrice,
	}, nil
}

func (p *PaperBroker) Orders(_ context.Context) ([]KiteOrder, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]KiteOrder, 0, len(p.orders))
	for id, order := range p.orders {
		out = append(out, KiteOrder{
			OrderID:         id,
			Exchange:        order.Exchange,
			TradingSymbol:   order.TradingSymbol,
			TransactionType: order.TransactionType,
			Quantity:        order.Quantity,
			Product:         order.Product,
			OrderType:       order.OrderType,
			Status:          p.status[id],
			Price:           order.Price,
			TriggerPrice:    order.TriggerPrice,
			Tag:             order.Tag,
		})
	}
	return out, nil
}

func (p *PaperBroker) Positions(_ context.Context) ([]Position, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]Position, len(p.positions))
	copy(out, p.positions)
	return out, nil
}

func (p *PaperBroker) CancelOrder(_ context.Context, _ string, orderID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.orders, orderID)
	return nil
}

func (p *PaperBroker) LTP(_ context.Context, exchange, symbol string) (float64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	k := key(exchange, symbol)
	price := p.ltp[k]
	if price == 0 {
		price = 100
	}
	price += float64(time.Now().UnixNano()%7-3) * 0.10
	p.ltp[k] = price
	return price, nil
}

func key(exchange, symbol string) string {
	return exchange + ":" + symbol
}
