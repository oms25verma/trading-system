package trading

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

type fakeBroker struct {
	nextID      int
	orders      map[string]Order
	status      map[string]string
	positions   []Position
	placeCount  int
	modifyCount int
	cancelCount int
	ltp         float64
}

func newFakeBroker() *fakeBroker {
	return &fakeBroker{
		orders: make(map[string]Order),
		status: make(map[string]string),
		ltp:    100,
	}
}

func (f *fakeBroker) PlaceOrder(_ context.Context, order Order) (string, error) {
	f.nextID++
	f.placeCount++
	id := "order-" + strconv.Itoa(f.nextID)
	f.orders[id] = order
	if order.OrderType == "MARKET" {
		f.status[id] = "COMPLETE"
	} else {
		f.status[id] = "OPEN"
	}
	return id, nil
}

func (f *fakeBroker) ModifyOrder(_ context.Context, _ string, orderID string, fields map[string]string) error {
	f.modifyCount++
	order, ok := f.orders[orderID]
	if !ok {
		return fmt.Errorf("order %s not found", orderID)
	}
	if value := fields["order_type"]; value != "" {
		order.OrderType = value
	}
	if value := fields["price"]; value != "" {
		order.Price, _ = strconv.ParseFloat(value, 64)
	}
	if value := fields["trigger_price"]; value != "" {
		order.TriggerPrice, _ = strconv.ParseFloat(value, 64)
	}
	f.orders[orderID] = order
	return nil
}

func (f *fakeBroker) CancelOrder(_ context.Context, _ string, orderID string) error {
	f.cancelCount++
	delete(f.orders, orderID)
	delete(f.status, orderID)
	return nil
}

func (f *fakeBroker) OrderStatus(_ context.Context, _ string, orderID string) (string, error) {
	status, ok := f.status[orderID]
	if !ok {
		return "", fmt.Errorf("order %s not found", orderID)
	}
	return status, nil
}

func (f *fakeBroker) OrderDetails(_ context.Context, _ string, orderID string) (*OrderDetails, error) {
	order, ok := f.orders[orderID]
	if !ok {
		return nil, fmt.Errorf("order %s not found", orderID)
	}
	return &OrderDetails{
		OrderID:      orderID,
		Status:       f.status[orderID],
		Price:        order.Price,
		TriggerPrice: order.TriggerPrice,
	}, nil
}

func (f *fakeBroker) Positions(_ context.Context) ([]Position, error) {
	out := make([]Position, len(f.positions))
	copy(out, f.positions)
	return out, nil
}

func (f *fakeBroker) LTP(_ context.Context, _, _ string) (float64, error) {
	return f.ltp, nil
}

func TestEnterWithOnlyTargetPlacesTarget(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)

	trade, err := manager.Enter(context.Background(), CreateTradeRequest{
		Exchange:      "MCX",
		TradingSymbol: "SILVERM26JUNFUT",
		Side:          "SELL",
		Quantity:      1,
		Product:       "MIS",
		OrderType:     "MARKET",
		Protection: &AutoProtectionRequest{
			ReferencePrice: 109000,
			TargetPoints:   40,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if trade.StopOrderID != "" || trade.StopLoss != nil {
		t.Fatalf("expected no stop-loss, got %+v", trade.StopLoss)
	}
	if trade.Target == nil || trade.Target.Price != 108960 {
		t.Fatalf("expected target 108960, got %+v", trade.Target)
	}
	if broker.placeCount != 2 {
		t.Fatalf("expected entry + target orders, got %d", broker.placeCount)
	}
}

func TestEnterWithOnlyStopLossAndOffset(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)

	trade, err := manager.Enter(context.Background(), CreateTradeRequest{
		Exchange:      "MCX",
		TradingSymbol: "SILVERM26JUNFUT",
		Side:          "SELL",
		Quantity:      1,
		Product:       "MIS",
		OrderType:     "MARKET",
		Protection: &AutoProtectionRequest{
			ReferencePrice: 109000,
			StopLossPoints: 20,
			SLLimitOffset:  5,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if trade.StopLoss == nil {
		t.Fatal("expected stop-loss")
	}
	if trade.StopLoss.TriggerPrice != 109020 || trade.StopLoss.LimitPrice != 109025 {
		t.Fatalf("unexpected stop-loss %+v", trade.StopLoss)
	}
	if trade.TargetOrderID != "" || trade.Target != nil {
		t.Fatalf("expected no target, got %+v", trade.Target)
	}
}

func TestEnterWithStopLossUsesDefaultLimitOffset(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)

	trade, err := manager.Enter(context.Background(), CreateTradeRequest{
		Exchange:      "MCX",
		TradingSymbol: "SILVERM26JUNFUT",
		Side:          "SELL",
		Quantity:      1,
		Product:       "MIS",
		OrderType:     "MARKET",
		Protection: &AutoProtectionRequest{
			ReferencePrice: 109000,
			StopLossPoints: 20,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if trade.StopLoss == nil || trade.StopLoss.TriggerPrice != 109020 || trade.StopLoss.LimitPrice != 109030 {
		t.Fatalf("unexpected stop-loss %+v", trade.StopLoss)
	}
}

func TestManualRiskValidationAgainstEntry(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)
	trade, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		EntryPrice:    20,
		EntryOrderID:  "kite-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := manager.AddStopLoss(context.Background(), trade.ID, StopLossRequest{TriggerPrice: 21, LimitPrice: 20.95}); err == nil {
		t.Fatal("expected invalid BUY stop-loss to fail")
	}
	if _, err := manager.AddTarget(context.Background(), trade.ID, TargetRequest{Price: 19}); err == nil {
		t.Fatal("expected invalid BUY target to fail")
	}
	if broker.placeCount != 0 {
		t.Fatalf("expected validation before broker call, got %d place calls", broker.placeCount)
	}
}

func TestLimitEntryDefersProtectionUntilComplete(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)

	trade, err := manager.Enter(context.Background(), CreateTradeRequest{
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		OrderType:     "LIMIT",
		Price:         100,
		Protection: &AutoProtectionRequest{
			StopLossPoints: 10,
			TargetPoints:   20,
			SLLimitOffset:  1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if broker.placeCount != 1 {
		t.Fatalf("expected only entry order before completion, got %d", broker.placeCount)
	}
	if trade.PendingProtection == nil {
		t.Fatal("expected pending protection")
	}

	manager.trailOnce(context.Background())
	if broker.placeCount != 1 {
		t.Fatalf("expected protection to remain deferred while open, got %d orders", broker.placeCount)
	}

	broker.status[trade.EntryOrderID] = "COMPLETE"
	manager.trailOnce(context.Background())
	trade = manager.List()[0]
	if trade.PendingProtection != nil {
		t.Fatalf("expected pending protection cleared, got %+v", trade.PendingProtection)
	}
	if trade.StopLoss == nil || trade.StopLoss.TriggerPrice != 90 || trade.StopLoss.LimitPrice != 89 {
		t.Fatalf("unexpected stop-loss %+v", trade.StopLoss)
	}
	if trade.Target == nil || trade.Target.Price != 120 {
		t.Fatalf("unexpected target %+v", trade.Target)
	}
	if broker.placeCount != 3 {
		t.Fatalf("expected entry + stop-loss + target, got %d", broker.placeCount)
	}
}

func TestManualStopLossForOpenLimitEntryIsDeferred(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)

	trade, err := manager.Enter(context.Background(), CreateTradeRequest{
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		OrderType:     "LIMIT",
		Price:         100,
	})
	if err != nil {
		t.Fatal(err)
	}

	trade, err = manager.AddStopLoss(context.Background(), trade.ID, StopLossRequest{TriggerPrice: 90, LimitPrice: 89})
	if err != nil {
		t.Fatal(err)
	}
	if trade.PendingStopLoss == nil || trade.StopOrderID != "" || trade.StopLoss != nil {
		t.Fatalf("expected pending stop-loss only, got %+v", trade)
	}
	if broker.placeCount != 1 {
		t.Fatalf("expected no stop-loss broker order while entry open, got %d orders", broker.placeCount)
	}

	broker.status[trade.EntryOrderID] = "COMPLETE"
	manager.trailOnce(context.Background())
	trade = manager.List()[0]
	if trade.PendingStopLoss != nil {
		t.Fatalf("expected pending stop-loss cleared, got %+v", trade.PendingStopLoss)
	}
	if trade.StopLoss == nil || trade.StopLoss.TriggerPrice != 90 || trade.StopOrderID == "" {
		t.Fatalf("expected live stop-loss after completion, got %+v", trade)
	}
	if broker.placeCount != 2 {
		t.Fatalf("expected entry + stop-loss orders, got %d", broker.placeCount)
	}
}

func TestManualTargetForOpenLimitEntryIsDeferred(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)

	trade, err := manager.Enter(context.Background(), CreateTradeRequest{
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		OrderType:     "LIMIT",
		Price:         100,
	})
	if err != nil {
		t.Fatal(err)
	}

	trade, err = manager.AddTarget(context.Background(), trade.ID, TargetRequest{Price: 120})
	if err != nil {
		t.Fatal(err)
	}
	if trade.PendingTarget == nil || trade.TargetOrderID != "" || trade.Target != nil {
		t.Fatalf("expected pending target only, got %+v", trade)
	}
	if broker.placeCount != 1 {
		t.Fatalf("expected no target broker order while entry open, got %d orders", broker.placeCount)
	}

	broker.status[trade.EntryOrderID] = "COMPLETE"
	manager.trailOnce(context.Background())
	trade = manager.List()[0]
	if trade.PendingTarget != nil {
		t.Fatalf("expected pending target cleared, got %+v", trade.PendingTarget)
	}
	if trade.Target == nil || trade.Target.Price != 120 || trade.TargetOrderID == "" {
		t.Fatalf("expected live target after completion, got %+v", trade)
	}
	if broker.placeCount != 2 {
		t.Fatalf("expected entry + target orders, got %d", broker.placeCount)
	}
}

func TestSetStopLossModifiesExistingOrder(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)
	trade, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "MCX",
		TradingSymbol: "SILVERM26JUNFUT",
		Side:          "SELL",
		Quantity:      1,
		Product:       "MIS",
		EntryPrice:    100,
		EntryOrderID:  "kite-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	trade, err = manager.AddStopLoss(context.Background(), trade.ID, StopLossRequest{TriggerPrice: 120, LimitPrice: 125})
	if err != nil {
		t.Fatal(err)
	}
	stopOrderID := trade.StopOrderID
	trade, err = manager.AddStopLoss(context.Background(), trade.ID, StopLossRequest{TriggerPrice: 115, LimitPrice: 116})
	if err != nil {
		t.Fatal(err)
	}

	if trade.StopOrderID != stopOrderID {
		t.Fatalf("expected same stop order id, got %s want %s", trade.StopOrderID, stopOrderID)
	}
	if broker.placeCount != 1 || broker.modifyCount != 1 {
		t.Fatalf("expected one place and one modify, got place=%d modify=%d", broker.placeCount, broker.modifyCount)
	}
	if trade.StopLoss.TriggerPrice != 115 || trade.StopLoss.LimitPrice != 116 {
		t.Fatalf("unexpected stop-loss %+v", trade.StopLoss)
	}
}

func TestSetTargetModifiesExistingOrder(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)
	trade, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		EntryPrice:    100,
		EntryOrderID:  "kite-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	trade, err = manager.AddTarget(context.Background(), trade.ID, TargetRequest{Price: 120})
	if err != nil {
		t.Fatal(err)
	}
	targetOrderID := trade.TargetOrderID
	trade, err = manager.AddTarget(context.Background(), trade.ID, TargetRequest{Price: 125})
	if err != nil {
		t.Fatal(err)
	}

	if trade.TargetOrderID != targetOrderID {
		t.Fatalf("expected same target order id, got %s want %s", trade.TargetOrderID, targetOrderID)
	}
	if broker.placeCount != 1 || broker.modifyCount != 1 {
		t.Fatalf("expected one place and one modify, got place=%d modify=%d", broker.placeCount, broker.modifyCount)
	}
	if trade.Target.Price != 125 {
		t.Fatalf("unexpected target %+v", trade.Target)
	}
}

func TestRemoveStopLossCancelsExistingOrder(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)
	trade, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "MCX",
		TradingSymbol: "SILVERM26JUNFUT",
		Side:          "SELL",
		Quantity:      1,
		Product:       "MIS",
		EntryPrice:    100,
		EntryOrderID:  "kite-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	trade, err = manager.AddStopLoss(context.Background(), trade.ID, StopLossRequest{TriggerPrice: 120, LimitPrice: 125})
	if err != nil {
		t.Fatal(err)
	}

	trade, err = manager.RemoveStopLoss(context.Background(), trade.ID)
	if err != nil {
		t.Fatal(err)
	}
	if trade.StopOrderID != "" || trade.StopLoss != nil {
		t.Fatalf("expected stop-loss cleared, got %+v", trade)
	}
	if broker.cancelCount != 1 {
		t.Fatalf("expected one cancel, got %d", broker.cancelCount)
	}
}

func TestRemoveTargetCancelsExistingOrder(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)
	trade, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		EntryPrice:    100,
		EntryOrderID:  "kite-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	trade, err = manager.AddTarget(context.Background(), trade.ID, TargetRequest{Price: 120})
	if err != nil {
		t.Fatal(err)
	}

	trade, err = manager.RemoveTarget(context.Background(), trade.ID)
	if err != nil {
		t.Fatal(err)
	}
	if trade.TargetOrderID != "" || trade.Target != nil {
		t.Fatalf("expected target cleared, got %+v", trade)
	}
	if broker.cancelCount != 1 {
		t.Fatalf("expected one cancel, got %d", broker.cancelCount)
	}
}

func TestRemovePendingStopLossForOpenLimitEntry(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)
	trade, err := manager.Enter(context.Background(), CreateTradeRequest{
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		OrderType:     "LIMIT",
		Price:         100,
	})
	if err != nil {
		t.Fatal(err)
	}
	trade, err = manager.AddStopLoss(context.Background(), trade.ID, StopLossRequest{TriggerPrice: 90, LimitPrice: 89})
	if err != nil {
		t.Fatal(err)
	}
	if trade.PendingStopLoss == nil {
		t.Fatal("expected pending stop-loss")
	}

	trade, err = manager.RemoveStopLoss(context.Background(), trade.ID)
	if err != nil {
		t.Fatal(err)
	}
	if trade.PendingStopLoss != nil || trade.StopLoss != nil || trade.StopOrderID != "" {
		t.Fatalf("expected stop-loss state cleared, got %+v", trade)
	}
	if broker.cancelCount != 0 {
		t.Fatalf("expected no broker cancel for pending stop-loss, got %d", broker.cancelCount)
	}

	broker.status[trade.EntryOrderID] = "COMPLETE"
	manager.trailOnce(context.Background())
	trade = manager.List()[0]
	if trade.StopLoss != nil || trade.StopOrderID != "" {
		t.Fatalf("expected removed pending stop-loss not to be created, got %+v", trade)
	}
}

func TestRemovePendingTargetForOpenLimitEntry(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)
	trade, err := manager.Enter(context.Background(), CreateTradeRequest{
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		OrderType:     "LIMIT",
		Price:         100,
	})
	if err != nil {
		t.Fatal(err)
	}
	trade, err = manager.AddTarget(context.Background(), trade.ID, TargetRequest{Price: 120})
	if err != nil {
		t.Fatal(err)
	}
	if trade.PendingTarget == nil {
		t.Fatal("expected pending target")
	}

	trade, err = manager.RemoveTarget(context.Background(), trade.ID)
	if err != nil {
		t.Fatal(err)
	}
	if trade.PendingTarget != nil || trade.Target != nil || trade.TargetOrderID != "" {
		t.Fatalf("expected target state cleared, got %+v", trade)
	}
	if broker.cancelCount != 0 {
		t.Fatalf("expected no broker cancel for pending target, got %d", broker.cancelCount)
	}

	broker.status[trade.EntryOrderID] = "COMPLETE"
	manager.trailOnce(context.Background())
	trade = manager.List()[0]
	if trade.Target != nil || trade.TargetOrderID != "" {
		t.Fatalf("expected removed pending target not to be created, got %+v", trade)
	}
}

func TestOCOStopLossCompleteCancelsTargetAndClosesTrade(t *testing.T) {
	broker := newFakeBroker()
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 1}}
	manager := NewManager(broker)
	trade, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		EntryPrice:    100,
		EntryOrderID:  "kite-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	trade, err = manager.AddStopLoss(context.Background(), trade.ID, StopLossRequest{TriggerPrice: 90, LimitPrice: 89})
	if err != nil {
		t.Fatal(err)
	}
	trade, err = manager.AddTarget(context.Background(), trade.ID, TargetRequest{Price: 120})
	if err != nil {
		t.Fatal(err)
	}

	broker.status[trade.StopOrderID] = OrderStatusComplete
	manager.trailOnce(context.Background())
	trade = manager.List()[0]

	if trade.TradeStatus != TradeStatusClosed || trade.ExitReason != ExitReasonStopLoss {
		t.Fatalf("expected stop-loss close, got %+v", trade)
	}
	if trade.TargetOrderStatus != OrderStatusCancelled {
		t.Fatalf("expected target cancelled, got %s", trade.TargetOrderStatus)
	}
	if broker.cancelCount != 1 {
		t.Fatalf("expected one sibling cancel, got %d", broker.cancelCount)
	}
}

func TestOCOTargetCompleteCancelsStopLossAndClosesTrade(t *testing.T) {
	broker := newFakeBroker()
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 1}}
	manager := NewManager(broker)
	trade, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		EntryPrice:    100,
		EntryOrderID:  "kite-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	trade, err = manager.AddStopLoss(context.Background(), trade.ID, StopLossRequest{TriggerPrice: 90, LimitPrice: 89})
	if err != nil {
		t.Fatal(err)
	}
	trade, err = manager.AddTarget(context.Background(), trade.ID, TargetRequest{Price: 120})
	if err != nil {
		t.Fatal(err)
	}

	broker.status[trade.TargetOrderID] = OrderStatusComplete
	manager.trailOnce(context.Background())
	trade = manager.List()[0]

	if trade.TradeStatus != TradeStatusClosed || trade.ExitReason != ExitReasonTarget {
		t.Fatalf("expected target close, got %+v", trade)
	}
	if trade.StopOrderStatus != OrderStatusCancelled {
		t.Fatalf("expected stop-loss cancelled, got %s", trade.StopOrderStatus)
	}
	if broker.cancelCount != 1 {
		t.Fatalf("expected one sibling cancel, got %d", broker.cancelCount)
	}
}

func TestOCOBothCompleteRaceClosesAsAmbiguousWithoutCancel(t *testing.T) {
	broker := newFakeBroker()
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 1}}
	manager := NewManager(broker)
	trade, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		EntryPrice:    100,
		EntryOrderID:  "kite-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	trade, err = manager.AddStopLoss(context.Background(), trade.ID, StopLossRequest{TriggerPrice: 90, LimitPrice: 89})
	if err != nil {
		t.Fatal(err)
	}
	trade, err = manager.AddTarget(context.Background(), trade.ID, TargetRequest{Price: 120})
	if err != nil {
		t.Fatal(err)
	}

	broker.status[trade.StopOrderID] = OrderStatusComplete
	broker.status[trade.TargetOrderID] = OrderStatusComplete
	manager.trailOnce(context.Background())
	trade = manager.List()[0]

	if trade.TradeStatus != TradeStatusClosed || trade.ExitReason != ExitReasonBothCompleted {
		t.Fatalf("expected ambiguous close, got %+v", trade)
	}
	if broker.cancelCount != 0 {
		t.Fatalf("expected no cancel when both complete, got %d", broker.cancelCount)
	}
}

func TestClosedTradeRejectsFurtherActions(t *testing.T) {
	broker := newFakeBroker()
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 1}}
	manager := NewManager(broker)
	trade, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		EntryPrice:    100,
		EntryOrderID:  "kite-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	trade, err = manager.AddStopLoss(context.Background(), trade.ID, StopLossRequest{TriggerPrice: 90, LimitPrice: 89})
	if err != nil {
		t.Fatal(err)
	}
	trade, err = manager.AddTarget(context.Background(), trade.ID, TargetRequest{Price: 120})
	if err != nil {
		t.Fatal(err)
	}
	broker.status[trade.TargetOrderID] = OrderStatusComplete
	manager.trailOnce(context.Background())

	if _, err := manager.AddStopLoss(context.Background(), trade.ID, StopLossRequest{TriggerPrice: 95, LimitPrice: 94}); err == nil {
		t.Fatal("expected closed trade to reject stop-loss")
	}
	if _, err := manager.AddTarget(context.Background(), trade.ID, TargetRequest{Price: 130}); err == nil {
		t.Fatal("expected closed trade to reject target")
	}
	if _, err := manager.Exit(context.Background(), trade.ID); err == nil {
		t.Fatal("expected closed trade to reject manual exit")
	}
}

func TestSingleStopLossCompleteClosesTrade(t *testing.T) {
	broker := newFakeBroker()
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 1}}
	manager := NewManager(broker)
	trade, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		EntryPrice:    100,
		EntryOrderID:  "kite-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	trade, err = manager.AddStopLoss(context.Background(), trade.ID, StopLossRequest{TriggerPrice: 90, LimitPrice: 89})
	if err != nil {
		t.Fatal(err)
	}
	broker.status[trade.StopOrderID] = OrderStatusComplete

	manager.trailOnce(context.Background())
	trade = manager.List()[0]
	if trade.TradeStatus != TradeStatusClosed || trade.ExitReason != ExitReasonStopLoss {
		t.Fatalf("expected single SL to close trade, got %+v", trade)
	}
}

func TestSingleTargetCompleteClosesTrade(t *testing.T) {
	broker := newFakeBroker()
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 1}}
	manager := NewManager(broker)
	trade, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		EntryPrice:    100,
		EntryOrderID:  "kite-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	trade, err = manager.AddTarget(context.Background(), trade.ID, TargetRequest{Price: 120})
	if err != nil {
		t.Fatal(err)
	}
	broker.status[trade.TargetOrderID] = OrderStatusComplete

	manager.trailOnce(context.Background())
	trade = manager.List()[0]
	if trade.TradeStatus != TradeStatusClosed || trade.ExitReason != ExitReasonTarget {
		t.Fatalf("expected single target to close trade, got %+v", trade)
	}
}

func TestManualKiteCancelClearsLocalExitOrder(t *testing.T) {
	broker := newFakeBroker()
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 1}}
	manager := NewManager(broker)
	trade, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		EntryPrice:    100,
		EntryOrderID:  "kite-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	trade, err = manager.AddStopLoss(context.Background(), trade.ID, StopLossRequest{TriggerPrice: 90, LimitPrice: 89})
	if err != nil {
		t.Fatal(err)
	}
	broker.status[trade.StopOrderID] = OrderStatusCancelled

	manager.trailOnce(context.Background())
	trade = manager.List()[0]
	if trade.TradeStatus == TradeStatusClosed {
		t.Fatalf("cancelled SL should not close trade, got %+v", trade)
	}
	if trade.StopOrderID != "" || trade.StopLoss != nil {
		t.Fatalf("expected cancelled SL cleared locally, got %+v", trade)
	}
}

func TestManualKiteModifyUpdatesLocalExitPrices(t *testing.T) {
	broker := newFakeBroker()
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 1}}
	manager := NewManager(broker)
	trade, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		EntryPrice:    100,
		EntryOrderID:  "kite-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	trade, err = manager.AddStopLoss(context.Background(), trade.ID, StopLossRequest{TriggerPrice: 90, LimitPrice: 89})
	if err != nil {
		t.Fatal(err)
	}
	broker.orders[trade.StopOrderID] = Order{Price: 87, TriggerPrice: 88}

	manager.trailOnce(context.Background())
	trade = manager.List()[0]
	if trade.StopLoss == nil || trade.StopLoss.TriggerPrice != 88 || trade.StopLoss.LimitPrice != 87 {
		t.Fatalf("expected local SL prices updated, got %+v", trade.StopLoss)
	}
}

func TestExternalManualPositionCloseCancelsExitsAndClosesTrade(t *testing.T) {
	broker := newFakeBroker()
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 1}}
	manager := NewManager(broker)
	trade, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		EntryPrice:    100,
		EntryOrderID:  "kite-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	trade, err = manager.AddStopLoss(context.Background(), trade.ID, StopLossRequest{TriggerPrice: 90, LimitPrice: 89})
	if err != nil {
		t.Fatal(err)
	}
	trade, err = manager.AddTarget(context.Background(), trade.ID, TargetRequest{Price: 120})
	if err != nil {
		t.Fatal(err)
	}
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 0}}

	manager.trailOnce(context.Background())
	trade = manager.List()[0]
	if trade.TradeStatus != TradeStatusClosed || trade.ExitReason != ExitReasonManualExternal {
		t.Fatalf("expected external manual close, got %+v", trade)
	}
	if trade.StopOrderStatus != OrderStatusCancelled || trade.TargetOrderStatus != OrderStatusCancelled {
		t.Fatalf("expected exits cancelled, got stop=%s target=%s", trade.StopOrderStatus, trade.TargetOrderStatus)
	}
	if broker.cancelCount != 2 {
		t.Fatalf("expected two exit cancels, got %d", broker.cancelCount)
	}
}

func TestImportRejectsDuplicates(t *testing.T) {
	manager := NewManager(newFakeBroker())
	_, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		EntryOrderID:  "kite-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		EntryOrderID:  "kite-2",
	}); err == nil {
		t.Fatal("expected duplicate id to fail")
	}
	if _, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t2",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		EntryOrderID:  "kite-1",
	}); err == nil {
		t.Fatal("expected duplicate entry_order_id to fail")
	}
}

func TestJSONStorePersistsAndReloadsTrades(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trades.json")
	manager, err := NewManagerWithStore(newFakeBroker(), NewJSONStore(path))
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "MCX",
		TradingSymbol: "SILVERM26JUNFUT",
		Side:          "SELL",
		Quantity:      1,
		Product:       "MIS",
		EntryPrice:    109000,
		EntryOrderID:  "kite-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewManagerWithStore(newFakeBroker(), NewJSONStore(path))
	if err != nil {
		t.Fatal(err)
	}
	trades := reloaded.List()
	if len(trades) != 1 {
		t.Fatalf("expected one reloaded trade, got %d", len(trades))
	}
	if trades[0].ID != "t1" || trades[0].EntryPrice != 109000 {
		t.Fatalf("unexpected reloaded trade %+v", trades[0])
	}
}

func TestDailyStorePathUsesDateWhenDirectoryProvided(t *testing.T) {
	got := dailyStorePath("/tmp/trading-data", time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC))
	want := filepath.Join("/tmp/trading-data", "trades_24_05_2026.json")
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}

	explicit := "/tmp/custom.json"
	if got := dailyStorePath(explicit, time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)); got != explicit {
		t.Fatalf("got %s want %s", got, explicit)
	}
}
