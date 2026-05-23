package trading

import (
	"context"
	"time"
)

type Broker interface {
	PlaceOrder(ctx context.Context, order Order) (string, error)
	ModifyOrder(ctx context.Context, variety, orderID string, fields map[string]string) error
	CancelOrder(ctx context.Context, variety, orderID string) error
	OrderStatus(ctx context.Context, variety, orderID string) (string, error)
	LTP(ctx context.Context, exchange, symbol string) (float64, error)
}

type Order struct {
	Exchange         string  `json:"exchange"`
	TradingSymbol    string  `json:"tradingsymbol"`
	TransactionType  string  `json:"transaction_type"`
	Quantity         int     `json:"quantity"`
	Product          string  `json:"product"`
	OrderType        string  `json:"order_type"`
	Variety          string  `json:"variety"`
	Validity         string  `json:"validity"`
	Price            float64 `json:"price,omitempty"`
	TriggerPrice     float64 `json:"trigger_price,omitempty"`
	MarketProtection *int    `json:"market_protection,omitempty"`
}

type ManagedTrade struct {
	ID                string             `json:"id"`
	Exchange          string             `json:"exchange"`
	TradingSymbol     string             `json:"tradingsymbol"`
	Side              string             `json:"side"`
	Quantity          int                `json:"quantity"`
	Product           string             `json:"product"`
	EntryPrice        float64            `json:"entry_price,omitempty"`
	EntryOrderID      string             `json:"entry_order_id"`
	EntryStatus       string             `json:"entry_status,omitempty"`
	ExitOrderID       string             `json:"exit_order_id,omitempty"`
	StopOrderID       string             `json:"stop_order_id,omitempty"`
	TargetOrderID     string             `json:"target_order_id,omitempty"`
	StopLoss          *StopLoss          `json:"stop_loss,omitempty"`
	Target            *Target            `json:"target,omitempty"`
	PendingProtection *PendingProtection `json:"pending_protection,omitempty"`
	PendingStopLoss   *StopLoss          `json:"pending_stop_loss,omitempty"`
	PendingTarget     *Target            `json:"pending_target,omitempty"`
	CreatedAt         time.Time          `json:"created_at"`
	UpdatedAt         time.Time          `json:"updated_at"`
}

type StopLoss struct {
	TriggerPrice float64 `json:"trigger_price"`
	LimitPrice   float64 `json:"limit_price"`
	TrailBy      float64 `json:"trail_by,omitempty"`
	HighestLTP   float64 `json:"highest_ltp,omitempty"`
	LowestLTP    float64 `json:"lowest_ltp,omitempty"`
}

type Target struct {
	Price float64 `json:"price"`
}

type PendingProtection struct {
	ReferencePrice float64 `json:"reference_price,omitempty"`
	StopLossPoints float64 `json:"stop_loss_points,omitempty"`
	TargetPoints   float64 `json:"target_points,omitempty"`
	TrailBy        float64 `json:"trail_by,omitempty"`
	SLLimitOffset  float64 `json:"sl_limit_offset,omitempty"`
}
