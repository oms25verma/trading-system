package trading

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

type fakeBroker struct {
	nextID      int
	orders      map[string]Order
	synced      []KiteOrder
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
	if value := fields["quantity"]; value != "" {
		order.Quantity, _ = strconv.Atoi(value)
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

func (f *fakeBroker) Orders(_ context.Context) ([]KiteOrder, error) {
	if f.synced != nil {
		out := make([]KiteOrder, len(f.synced))
		for i, order := range f.synced {
			out[i] = order
			if _, ok := f.orders[order.OrderID]; !ok {
				f.orders[order.OrderID] = Order{
					Exchange:        order.Exchange,
					TradingSymbol:   order.TradingSymbol,
					TransactionType: order.TransactionType,
					Quantity:        order.Quantity,
					Product:         order.Product,
					OrderType:       order.OrderType,
					Price:           order.Price,
					TriggerPrice:    order.TriggerPrice,
					Tag:             order.Tag,
				}
				f.status[order.OrderID] = order.Status
			}
		}
		return out, nil
	}
	out := make([]KiteOrder, 0, len(f.orders))
	for id, order := range f.orders {
		out = append(out, KiteOrder{
			OrderID:         id,
			Exchange:        order.Exchange,
			TradingSymbol:   order.TradingSymbol,
			TransactionType: order.TransactionType,
			Quantity:        order.Quantity,
			Product:         order.Product,
			OrderType:       order.OrderType,
			Status:          f.status[id],
			Price:           order.Price,
			TriggerPrice:    order.TriggerPrice,
			Tag:             order.Tag,
		})
	}
	return out, nil
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

func TestListGroupsAggregatesOpenSameSideTrades(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)

	first, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "nse",
		TradingSymbol: "infy",
		Side:          "BUY",
		Quantity:      1,
		Product:       "mis",
		EntryPrice:    100,
		EntryOrderID:  "kite-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t2",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      3,
		Product:       "MIS",
		EntryPrice:    104,
		EntryOrderID:  "kite-2",
	})
	if err != nil {
		t.Fatal(err)
	}

	groups := manager.ListGroups()
	if len(groups) != 1 {
		t.Fatalf("expected one group, got %d", len(groups))
	}
	group := groups[0]
	if group.ID != "NSE:INFY:MIS" || group.Side != "BUY" {
		t.Fatalf("unexpected group identity: %+v", group)
	}
	if group.Quantity != 4 {
		t.Fatalf("expected quantity 4, got %d", group.Quantity)
	}
	if group.AverageEntryPrice != 103 {
		t.Fatalf("expected average price 103, got %f", group.AverageEntryPrice)
	}
	if len(group.TradeIDs) != 2 || group.TradeIDs[0] != first.ID || group.TradeIDs[1] != second.ID {
		t.Fatalf("unexpected trade ids: %+v", group.TradeIDs)
	}
}

func TestEnterRejectsOppositeSideWhenActiveGroupExists(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)

	_, err := manager.Import(context.Background(), ImportTradeRequest{
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

	_, err = manager.Enter(context.Background(), CreateTradeRequest{
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "SELL",
		Quantity:      1,
		Product:       "MIS",
		OrderType:     "MARKET",
	})
	if err == nil {
		t.Fatal("expected opposite-side entry to fail")
	}
	var domainErr *DomainError
	if !errors.As(err, &domainErr) || domainErr.Code != "opposite_side_active_group" {
		t.Fatalf("expected opposite_side_active_group, got %v", err)
	}
	if broker.placeCount != 0 {
		t.Fatalf("expected validation before broker call, got %d place calls", broker.placeCount)
	}
}

func TestLocalSystemOrdersUseTag(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)

	trade, err := manager.Enter(context.Background(), CreateTradeRequest{
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		OrderType:     "MARKET",
	})
	if err != nil {
		t.Fatal(err)
	}
	if broker.orders[trade.EntryOrderID].Tag != LocalSystemOrderTag {
		t.Fatalf("expected entry order tag %q, got %q", LocalSystemOrderTag, broker.orders[trade.EntryOrderID].Tag)
	}

	trade, err = manager.AddStopLoss(context.Background(), trade.ID, StopLossRequest{TriggerPrice: 90, LimitPrice: 89})
	if err != nil {
		t.Fatal(err)
	}
	if broker.orders[trade.StopOrderID].Tag != LocalSystemOrderTag {
		t.Fatalf("expected stop-loss order tag %q, got %q", LocalSystemOrderTag, broker.orders[trade.StopOrderID].Tag)
	}

	trade, err = manager.AddTarget(context.Background(), trade.ID, TargetRequest{Price: 120})
	if err != nil {
		t.Fatal(err)
	}
	if broker.orders[trade.TargetOrderID].Tag != LocalSystemOrderTag {
		t.Fatalf("expected target order tag %q, got %q", LocalSystemOrderTag, broker.orders[trade.TargetOrderID].Tag)
	}

	trade, err = manager.Exit(context.Background(), trade.ID)
	if err != nil {
		t.Fatal(err)
	}
	if broker.orders[trade.ExitOrderID].Tag != LocalSystemOrderTag {
		t.Fatalf("expected exit order tag %q, got %q", LocalSystemOrderTag, broker.orders[trade.ExitOrderID].Tag)
	}
}

func TestSyncKiteOrdersInfersCreationSource(t *testing.T) {
	broker := newFakeBroker()
	broker.synced = []KiteOrder{
		{
			OrderID:         "local-1",
			Exchange:        "NSE",
			TradingSymbol:   "INFY",
			TransactionType: "BUY",
			Quantity:        1,
			Product:         "MIS",
			OrderType:       "MARKET",
			Status:          OrderStatusComplete,
			Tag:             LocalSystemOrderTag,
		},
		{
			OrderID:         "kite-1",
			Exchange:        "NSE",
			TradingSymbol:   "TCS",
			TransactionType: "BUY",
			Quantity:        1,
			Product:         "MIS",
			OrderType:       "MARKET",
			Status:          OrderStatusComplete,
		},
		{
			OrderID:         "other-1",
			Exchange:        "NSE",
			TradingSymbol:   "SBIN",
			TransactionType: "BUY",
			Quantity:        1,
			Product:         "MIS",
			OrderType:       "MARKET",
			Status:          OrderStatusComplete,
			Tag:             "OTHER",
		},
	}
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 1}}
	manager := NewManager(broker)

	result, err := manager.SyncKite(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.OrdersSynced != 3 || result.PositionsSynced != 1 || result.LocalSystemOrders != 1 || result.ExternalOrders != 2 {
		t.Fatalf("unexpected sync result: %+v", result)
	}

	orders := manager.ListSyncedOrders()
	if len(orders) != 3 {
		t.Fatalf("expected 3 synced orders, got %d", len(orders))
	}
	sources := map[string]string{}
	for _, order := range orders {
		sources[order.OrderID] = order.CreationSource
		if order.SyncedAt.IsZero() {
			t.Fatalf("expected synced_at on %+v", order)
		}
	}
	if sources["local-1"] != CreationSourceLocalSystem {
		t.Fatalf("expected local source, got %q", sources["local-1"])
	}
	if sources["kite-1"] != CreationSourceKiteApp {
		t.Fatalf("expected kite app source, got %q", sources["kite-1"])
	}
	if sources["other-1"] != CreationSourceUnknown {
		t.Fatalf("expected unknown source, got %q", sources["other-1"])
	}
	positions := manager.ListSyncedPositions()
	if len(positions) != 1 || positions[0].TradingSymbol != "INFY" || positions[0].Quantity != 1 {
		t.Fatalf("unexpected synced positions: %+v", positions)
	}
}

func TestListGroupsWarnsForOppositeExposureAcrossProducts(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)

	_, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "mis-buy",
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
	_, err = manager.Import(context.Background(), ImportTradeRequest{
		ID:            "nrml-sell",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "SELL",
		Quantity:      1,
		Product:       "NRML",
		EntryPrice:    101,
		EntryOrderID:  "kite-2",
	})
	if err != nil {
		t.Fatal(err)
	}

	groups := manager.ListGroups()
	if len(groups) != 2 {
		t.Fatalf("expected two groups, got %d", len(groups))
	}
	for _, group := range groups {
		if len(group.Warnings) != 1 || group.Warnings[0] != "OPPOSITE_EXPOSURE_ACROSS_PRODUCTS" {
			t.Fatalf("expected opposite exposure warning for %+v", group)
		}
	}
}

func TestSyncKiteCreatesUnmanagedGroupForExternalPosition(t *testing.T) {
	broker := newFakeBroker()
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 2}}
	manager := NewManager(broker)

	result, err := manager.SyncKite(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.PositionsAdded != 1 {
		t.Fatalf("expected one added position, got %+v", result)
	}

	groups := manager.ListGroups()
	if len(groups) != 1 {
		t.Fatalf("expected one group, got %d", len(groups))
	}
	group := groups[0]
	if group.ID != "NSE:INFY:MIS" || group.Side != "BUY" || group.Quantity != 2 {
		t.Fatalf("unexpected external group: %+v", group)
	}
	if group.CreationSource != CreationSourceKiteApp || group.ManagementStatus != ManagementStatusUnmanaged {
		t.Fatalf("unexpected source/status: %+v", group)
	}
}

func TestSyncKiteAppliesPartialExternalExitToSingleManagedTrade(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)
	trade, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      3,
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
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 3}}
	if _, err := manager.SyncKite(context.Background()); err != nil {
		t.Fatal(err)
	}
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 1}}
	result, err := manager.SyncKite(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.PositionsChanged != 1 {
		t.Fatalf("expected one changed position, got %+v", result)
	}
	trades := manager.List()
	if len(trades) != 1 || trades[0].Quantity != 1 || trades[0].InitialQuantity != 3 {
		t.Fatalf("expected remaining quantity 1 and initial quantity 3, got %+v", trades)
	}
	if broker.orders[trade.StopOrderID].Quantity != 1 || broker.orders[trade.TargetOrderID].Quantity != 1 {
		t.Fatalf("expected protection resized to 1, got stop=%+v target=%+v", broker.orders[trade.StopOrderID], broker.orders[trade.TargetOrderID])
	}

	groups := manager.ListGroups()
	if len(groups) != 1 {
		t.Fatalf("expected one group, got %d", len(groups))
	}
	group := groups[0]
	if group.Quantity != 1 || group.LocalQuantity != 1 || group.BrokerQuantity != 1 {
		t.Fatalf("unexpected quantities: %+v", group)
	}
	if group.ManagementStatus != ManagementStatusManaged || len(group.Warnings) != 0 {
		t.Fatalf("expected reconciled managed group, got %+v", group)
	}
}

func TestSyncKiteClosesSingleManagedTradeAfterExternalFlatten(t *testing.T) {
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
	trade, err = manager.AddStopLoss(context.Background(), trade.ID, StopLossRequest{TriggerPrice: 90, LimitPrice: 89})
	if err != nil {
		t.Fatal(err)
	}
	trade, err = manager.AddTarget(context.Background(), trade.ID, TargetRequest{Price: 120})
	if err != nil {
		t.Fatal(err)
	}
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 1}}
	if _, err := manager.SyncKite(context.Background()); err != nil {
		t.Fatal(err)
	}
	broker.positions = nil
	result, err := manager.SyncKite(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.PositionsRemoved != 1 {
		t.Fatalf("expected one removed position, got %+v", result)
	}

	trades := manager.List()
	if len(trades) != 1 || trades[0].TradeStatus != TradeStatusClosed || trades[0].ExitReason != ExitReasonManualExternal {
		t.Fatalf("expected external close, got %+v", trades)
	}
	if broker.cancelCount != 2 {
		t.Fatalf("expected stop-loss and target cancellation, got %d", broker.cancelCount)
	}
}

func TestSyncKiteDoesNotAllocatePartialExitAcrossMultipleTrades(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)
	for _, id := range []string{"t1", "t2"} {
		_, err := manager.Import(context.Background(), ImportTradeRequest{
			ID:            id,
			Exchange:      "NSE",
			TradingSymbol: "INFY",
			Side:          "BUY",
			Quantity:      1,
			Product:       "MIS",
			EntryPrice:    100,
			EntryOrderID:  "kite-" + id,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 2}}
	if _, err := manager.SyncKite(context.Background()); err != nil {
		t.Fatal(err)
	}
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 1}}
	if _, err := manager.SyncKite(context.Background()); err != nil {
		t.Fatal(err)
	}

	trades := manager.List()
	if len(trades) != 2 || trades[0].Quantity != 1 || trades[1].Quantity != 1 {
		t.Fatalf("expected ambiguous trades to remain unchanged, got %+v", trades)
	}
	groups := manager.ListGroups()
	if len(groups) != 1 || groups[0].ManagementStatus != ManagementStatusPartiallyManaged {
		t.Fatalf("expected partially managed group, got %+v", groups)
	}
}

func TestSyncKiteDetectsProductConversionWithoutClosingTrade(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)
	trade, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "MCX",
		TradingSymbol: "SILVERM26JUNFUT",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		EntryPrice:    100,
		EntryOrderID:  "kite-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	broker.positions = []Position{{Exchange: "MCX", TradingSymbol: "SILVERM26JUNFUT", Product: "MIS", Quantity: 1}}
	if _, err := manager.SyncKite(context.Background()); err != nil {
		t.Fatal(err)
	}
	broker.positions = []Position{{Exchange: "MCX", TradingSymbol: "SILVERM26JUNFUT", Product: "NRML", Quantity: 1}}
	result, err := manager.SyncKite(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.ProductConversionsDetected != 1 {
		t.Fatalf("expected one conversion, got %+v", result)
	}
	if manager.List()[0].TradeStatus != TradeStatusOpen {
		t.Fatalf("expected trade to remain open after conversion detection, got %+v", manager.List()[0])
	}

	groups := manager.ListGroups()
	if len(groups) != 2 {
		t.Fatalf("expected old and converted groups, got %+v", groups)
	}
	var oldGroup, newGroup *PositionGroup
	for _, group := range groups {
		switch group.Product {
		case "MIS":
			oldGroup = group
		case "NRML":
			newGroup = group
		}
	}
	if oldGroup == nil || oldGroup.ConvertedToProduct != "NRML" || !contains(oldGroup.Warnings, WarningProductConversionDetected) {
		t.Fatalf("expected old group conversion warning, got %+v", oldGroup)
	}
	if newGroup == nil || newGroup.ConvertedFromProduct != "MIS" || !contains(newGroup.Warnings, WarningProductConversionDetected) {
		t.Fatalf("expected new group conversion warning, got %+v", newGroup)
	}

	if _, err := manager.AddTarget(context.Background(), trade.ID, TargetRequest{Price: 120}); err == nil {
		t.Fatal("expected stale-product target action to fail")
	} else {
		var domainErr *DomainError
		if !errors.As(err, &domainErr) || domainErr.Code != "product_conversion_detected" {
			t.Fatalf("expected product_conversion_detected, got %v", err)
		}
	}

	if _, err := manager.SyncKite(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Exit(context.Background(), trade.ID); err == nil {
		t.Fatal("expected conversion guard to survive later sync")
	}
}

func TestApplyDetectedProductConversionRecreatesProtectionWithNewProduct(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)
	trade, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "MCX",
		TradingSymbol: "SILVERM26JUNFUT",
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
	oldStopOrderID := trade.StopOrderID
	oldTargetOrderID := trade.TargetOrderID

	broker.positions = []Position{{Exchange: "MCX", TradingSymbol: "SILVERM26JUNFUT", Product: "MIS", Quantity: 1}}
	if _, err := manager.SyncKite(context.Background()); err != nil {
		t.Fatal(err)
	}
	broker.positions = []Position{{Exchange: "MCX", TradingSymbol: "SILVERM26JUNFUT", Product: "NRML", Quantity: 1}}
	if _, err := manager.SyncKite(context.Background()); err != nil {
		t.Fatal(err)
	}

	trade, err = manager.ApplyDetectedProductConversion(context.Background(), trade.ID)
	if err != nil {
		t.Fatal(err)
	}
	if trade.Product != "NRML" {
		t.Fatalf("expected NRML product, got %+v", trade)
	}
	if trade.StopOrderID == "" || trade.StopOrderID == oldStopOrderID || broker.orders[trade.StopOrderID].Product != "NRML" {
		t.Fatalf("expected recreated NRML stop-loss, got %+v", trade)
	}
	if trade.TargetOrderID == "" || trade.TargetOrderID == oldTargetOrderID || broker.orders[trade.TargetOrderID].Product != "NRML" {
		t.Fatalf("expected recreated NRML target, got %+v", trade)
	}
	if _, ok := broker.orders[oldStopOrderID]; ok {
		t.Fatalf("expected old stop-loss order %s cancelled", oldStopOrderID)
	}
	if _, ok := broker.orders[oldTargetOrderID]; ok {
		t.Fatalf("expected old target order %s cancelled", oldTargetOrderID)
	}
	if _, err := manager.AddTarget(context.Background(), trade.ID, TargetRequest{Price: 125}); err != nil {
		t.Fatalf("expected target modification after migration, got %v", err)
	}
	groups := manager.ListGroups()
	if len(groups) != 1 || groups[0].Product != "NRML" || groups[0].ManagementStatus != ManagementStatusManaged {
		t.Fatalf("expected migrated managed group, got %+v", groups)
	}
}

func TestGroupActionsDelegateToSingleManagedTrade(t *testing.T) {
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

	trade, err = manager.AddGroupStopLoss(context.Background(), "nse:infy:mis", StopLossRequest{TriggerPrice: 90, LimitPrice: 89})
	if err != nil {
		t.Fatal(err)
	}
	if trade.StopOrderID == "" {
		t.Fatal("expected group stop-loss")
	}
	trade, err = manager.AddGroupTarget(context.Background(), "NSE:INFY:MIS", TargetRequest{Price: 120})
	if err != nil {
		t.Fatal(err)
	}
	if trade.TargetOrderID == "" {
		t.Fatal("expected group target")
	}
	trade, err = manager.RemoveGroupStopLoss(context.Background(), "NSE:INFY:MIS")
	if err != nil {
		t.Fatal(err)
	}
	if trade.StopOrderID != "" {
		t.Fatalf("expected group stop-loss removal, got %+v", trade)
	}
	trade, err = manager.RemoveGroupTarget(context.Background(), "NSE:INFY:MIS")
	if err != nil {
		t.Fatal(err)
	}
	if trade.TargetOrderID != "" {
		t.Fatalf("expected group target removal, got %+v", trade)
	}
	trade, err = manager.ExitGroup(context.Background(), "NSE:INFY:MIS")
	if err != nil {
		t.Fatal(err)
	}
	if trade.TradeStatus != TradeStatusClosed || trade.ExitReason != ExitReasonManual {
		t.Fatalf("expected group exit, got %+v", trade)
	}
}

func TestGroupActionRejectsUnmanagedExternalPosition(t *testing.T) {
	broker := newFakeBroker()
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 1}}
	manager := NewManager(broker)
	if _, err := manager.SyncKite(context.Background()); err != nil {
		t.Fatal(err)
	}

	_, err := manager.AddGroupTarget(context.Background(), "NSE:INFY:MIS", TargetRequest{Price: 120})
	var domainErr *DomainError
	if !errors.As(err, &domainErr) || domainErr.Code != "group_unmanaged" {
		t.Fatalf("expected group_unmanaged, got %v", err)
	}
}

func TestGroupActionRejectsMultipleLocalTrades(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)
	for _, id := range []string{"t1", "t2"} {
		_, err := manager.Import(context.Background(), ImportTradeRequest{
			ID:            id,
			Exchange:      "NSE",
			TradingSymbol: "INFY",
			Side:          "BUY",
			Quantity:      1,
			Product:       "MIS",
			EntryPrice:    100,
			EntryOrderID:  "kite-" + id,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	_, err := manager.ExitGroup(context.Background(), "NSE:INFY:MIS")
	var domainErr *DomainError
	if !errors.As(err, &domainErr) || domainErr.Code != "ambiguous_group_trades" {
		t.Fatalf("expected ambiguous_group_trades, got %v", err)
	}
}

func TestTakeOverExternalGroupEnablesManagedActions(t *testing.T) {
	broker := newFakeBroker()
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: -2}}
	manager := NewManager(broker)
	if _, err := manager.SyncKite(context.Background()); err != nil {
		t.Fatal(err)
	}

	trade, err := manager.TakeOverGroup(context.Background(), "nse:infy:mis", TakeOverGroupRequest{EntryPrice: 100})
	if err != nil {
		t.Fatal(err)
	}
	if trade.Side != "SELL" || trade.Quantity != 2 || trade.EntryPrice != 100 || trade.CreationSource != CreationSourceKiteApp {
		t.Fatalf("unexpected taken-over trade: %+v", trade)
	}
	if broker.placeCount != 0 {
		t.Fatalf("expected no entry order during take-over, got %d place calls", broker.placeCount)
	}

	trade, err = manager.AddGroupStopLoss(context.Background(), "NSE:INFY:MIS", StopLossRequest{TriggerPrice: 110, LimitPrice: 111})
	if err != nil {
		t.Fatal(err)
	}
	if trade.StopOrderID == "" || broker.orders[trade.StopOrderID].Product != "MIS" || broker.orders[trade.StopOrderID].Quantity != 2 {
		t.Fatalf("expected managed stop-loss after take-over, got %+v", trade)
	}

	groups := manager.ListGroups()
	if len(groups) != 1 || groups[0].ManagementStatus != ManagementStatusManaged || groups[0].CreationSource != CreationSourceKiteApp {
		t.Fatalf("expected managed Kite-app group, got %+v", groups)
	}
}

func TestTakeOverGroupRejectsAlreadyManagedGroup(t *testing.T) {
	broker := newFakeBroker()
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 1}}
	manager := NewManager(broker)
	if _, err := manager.SyncKite(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.TakeOverGroup(context.Background(), "NSE:INFY:MIS", TakeOverGroupRequest{EntryPrice: 100}); err != nil {
		t.Fatal(err)
	}

	_, err := manager.TakeOverGroup(context.Background(), "NSE:INFY:MIS", TakeOverGroupRequest{EntryPrice: 100})
	var domainErr *DomainError
	if !errors.As(err, &domainErr) || domainErr.Code != "group_already_managed" {
		t.Fatalf("expected group_already_managed, got %v", err)
	}
}

func TestSyncKiteLinksUnambiguousExternalExitOrders(t *testing.T) {
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
		EntryOrderID:  "kite-entry",
	})
	if err != nil {
		t.Fatal(err)
	}
	broker.synced = []KiteOrder{
		{
			OrderID:         "ext-sl",
			Exchange:        "NSE",
			TradingSymbol:   "INFY",
			TransactionType: "SELL",
			Quantity:        1,
			Product:         "MIS",
			OrderType:       "SL",
			Status:          OrderStatusOpen,
			Price:           89,
			TriggerPrice:    90,
		},
		{
			OrderID:         "ext-target",
			Exchange:        "NSE",
			TradingSymbol:   "INFY",
			TransactionType: "SELL",
			Quantity:        1,
			Product:         "MIS",
			OrderType:       "LIMIT",
			Status:          OrderStatusOpen,
			Price:           120,
		},
	}
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 1}}

	result, err := manager.SyncKite(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.ExternalStopLossesLinked != 1 || result.ExternalTargetsLinked != 1 {
		t.Fatalf("expected external exits linked, got %+v", result)
	}
	trade = manager.List()[0]
	if trade.StopOrderID != "ext-sl" || trade.StopLoss == nil || trade.StopLoss.TriggerPrice != 90 || trade.StopLoss.LimitPrice != 89 {
		t.Fatalf("expected linked external SL, got %+v", trade)
	}
	if trade.TargetOrderID != "ext-target" || trade.Target == nil || trade.Target.Price != 120 {
		t.Fatalf("expected linked external target, got %+v", trade)
	}
	trade, err = manager.AddTarget(context.Background(), trade.ID, TargetRequest{Price: 125})
	if err != nil {
		t.Fatal(err)
	}
	if trade.TargetOrderID != "ext-target" || broker.modifyCount != 1 {
		t.Fatalf("expected linked external target to be modified, got trade=%+v modifyCount=%d", trade, broker.modifyCount)
	}
}

func TestSyncKiteDoesNotLinkAmbiguousExternalExitOrders(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)
	_, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		EntryPrice:    100,
		EntryOrderID:  "kite-entry",
	})
	if err != nil {
		t.Fatal(err)
	}
	broker.synced = []KiteOrder{
		{OrderID: "ext-sl-1", Exchange: "NSE", TradingSymbol: "INFY", TransactionType: "SELL", Quantity: 1, Product: "MIS", OrderType: "SL", Status: OrderStatusOpen, Price: 89, TriggerPrice: 90},
		{OrderID: "ext-sl-2", Exchange: "NSE", TradingSymbol: "INFY", TransactionType: "SELL", Quantity: 1, Product: "MIS", OrderType: "SL", Status: OrderStatusOpen, Price: 88, TriggerPrice: 89},
	}
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 1}}

	result, err := manager.SyncKite(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.ExternalStopLossesLinked != 0 {
		t.Fatalf("expected no ambiguous SL link, got %+v", result)
	}
	if result.AmbiguousExternalExits != 1 {
		t.Fatalf("expected one ambiguous external exit, got %+v", result)
	}
	trade := manager.List()[0]
	if trade.StopOrderID != "" || trade.StopLoss != nil {
		t.Fatalf("expected no linked stop-loss, got %+v", trade)
	}
	groups := manager.ListGroups()
	if len(groups) != 1 {
		t.Fatalf("expected one group, got %d", len(groups))
	}
	if groups[0].ManagementStatus != ManagementStatusConflict || !contains(groups[0].Warnings, WarningAmbiguousExternalStopLoss) {
		t.Fatalf("expected conflict group with ambiguous SL warning, got %+v", groups[0])
	}
}

func TestSyncKiteIgnoresTaggedLocalOrdersForExternalExitConflicts(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)
	_, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		EntryPrice:    100,
		EntryOrderID:  "kite-entry",
	})
	if err != nil {
		t.Fatal(err)
	}
	broker.synced = []KiteOrder{
		{OrderID: "local-sl-1", Exchange: "NSE", TradingSymbol: "INFY", TransactionType: "SELL", Quantity: 1, Product: "MIS", OrderType: "SL", Status: OrderStatusOpen, Price: 89, TriggerPrice: 90, Tag: LocalSystemOrderTag},
		{OrderID: "local-sl-2", Exchange: "NSE", TradingSymbol: "INFY", TransactionType: "SELL", Quantity: 1, Product: "MIS", OrderType: "SL", Status: OrderStatusOpen, Price: 88, TriggerPrice: 89, Tag: LocalSystemOrderTag},
	}
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 1}}

	result, err := manager.SyncKite(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.AmbiguousExternalExits != 0 || result.ExternalStopLossesLinked != 0 {
		t.Fatalf("expected tagged local orders to be ignored, got %+v", result)
	}
	groups := manager.ListGroups()
	if len(groups) != 1 || groups[0].ManagementStatus != ManagementStatusManaged || contains(groups[0].Warnings, WarningAmbiguousExternalStopLoss) {
		t.Fatalf("expected managed group without external conflict, got %+v", groups)
	}
}

func TestDashboardSummaryReportsHealthyManagedState(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)
	if _, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		EntryPrice:    100,
		EntryOrderID:  "kite-entry",
	}); err != nil {
		t.Fatal(err)
	}

	summary := manager.DashboardSummary()
	if summary.RiskStatus != "OK" {
		t.Fatalf("expected OK risk status, got %+v", summary)
	}
	if summary.ActiveGroups != 1 || summary.ManagedGroups != 1 || summary.OpenTrades != 1 || summary.ClosedTrades != 0 {
		t.Fatalf("unexpected dashboard counts: %+v", summary)
	}
	if summary.ConflictGroups != 0 || summary.WarningGroups != 0 || summary.UnmanagedGroups != 0 {
		t.Fatalf("expected no warnings/conflicts, got %+v", summary)
	}
}

func TestDashboardSummaryReportsConflictsAndRejectedOrders(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)
	if _, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		EntryPrice:    100,
		EntryOrderID:  "kite-entry",
	}); err != nil {
		t.Fatal(err)
	}
	broker.synced = []KiteOrder{
		{OrderID: "ext-sl-1", Exchange: "NSE", TradingSymbol: "INFY", TransactionType: "SELL", Quantity: 1, Product: "MIS", OrderType: "SL", Status: OrderStatusOpen, Price: 89, TriggerPrice: 90},
		{OrderID: "ext-sl-2", Exchange: "NSE", TradingSymbol: "INFY", TransactionType: "SELL", Quantity: 1, Product: "MIS", OrderType: "SL", Status: OrderStatusOpen, Price: 88, TriggerPrice: 89},
		{OrderID: "rejected-1", Exchange: "NSE", TradingSymbol: "SBIN", TransactionType: "BUY", Quantity: 1, Product: "MIS", OrderType: "MARKET", Status: OrderStatusRejected},
	}
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 1}}
	if _, err := manager.SyncKite(context.Background()); err != nil {
		t.Fatal(err)
	}

	summary := manager.DashboardSummary()
	if summary.RiskStatus != "CONFLICT" {
		t.Fatalf("expected CONFLICT risk status, got %+v", summary)
	}
	if summary.ConflictGroups != 1 || len(summary.Conflicts) != 1 {
		t.Fatalf("expected one conflict group, got %+v", summary)
	}
	if summary.Warnings[WarningAmbiguousExternalStopLoss] != 1 {
		t.Fatalf("expected ambiguous SL warning count, got %+v", summary.Warnings)
	}
	if summary.RejectedOrders != 1 || len(summary.RecentRejectedOrders) != 1 {
		t.Fatalf("expected rejected order summary, got %+v", summary)
	}
	if summary.OpenOrders != 2 || len(summary.RecentOpenOrders) != 2 {
		t.Fatalf("expected two open orders, got %+v", summary)
	}
}

func TestListConflictsReturnsGroupsNeedingAttention(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)
	if _, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		EntryPrice:    100,
		EntryOrderID:  "kite-entry",
	}); err != nil {
		t.Fatal(err)
	}
	broker.synced = []KiteOrder{
		{OrderID: "ext-sl-1", Exchange: "NSE", TradingSymbol: "INFY", TransactionType: "SELL", Quantity: 1, Product: "MIS", OrderType: "SL", Status: OrderStatusOpen, Price: 89, TriggerPrice: 90},
		{OrderID: "ext-sl-2", Exchange: "NSE", TradingSymbol: "INFY", TransactionType: "SELL", Quantity: 1, Product: "MIS", OrderType: "SL", Status: OrderStatusOpen, Price: 88, TriggerPrice: 89},
	}
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 1}}
	if _, err := manager.SyncKite(context.Background()); err != nil {
		t.Fatal(err)
	}

	conflicts := manager.ListConflicts()
	if len(conflicts) != 1 {
		t.Fatalf("expected one conflict group, got %d: %+v", len(conflicts), conflicts)
	}
	if conflicts[0].ID != "NSE:INFY:MIS" || conflicts[0].ManagementStatus != ManagementStatusConflict {
		t.Fatalf("unexpected conflict group: %+v", conflicts[0])
	}
}

func TestDetailGettersReturnSyncedState(t *testing.T) {
	broker := newFakeBroker()
	broker.synced = []KiteOrder{
		{OrderID: "order-1", Exchange: "NSE", TradingSymbol: "INFY", TransactionType: "BUY", Quantity: 1, Product: "MIS", OrderType: "MARKET", Status: OrderStatusComplete},
	}
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 1}}
	manager := NewManager(broker)
	if _, err := manager.SyncKite(context.Background()); err != nil {
		t.Fatal(err)
	}

	order, err := manager.GetSyncedOrder("order-1")
	if err != nil {
		t.Fatal(err)
	}
	if order.OrderID != "order-1" || order.TradingSymbol != "INFY" {
		t.Fatalf("unexpected order detail: %+v", order)
	}
	position, err := manager.GetSyncedPosition("nse:infy:mis")
	if err != nil {
		t.Fatal(err)
	}
	if position.TradingSymbol != "INFY" || position.Quantity != 1 {
		t.Fatalf("unexpected position detail: %+v", position)
	}
	group, err := manager.GetGroup("nse:infy:mis")
	if err != nil {
		t.Fatal(err)
	}
	if group.ID != "NSE:INFY:MIS" || group.ManagementStatus != ManagementStatusUnmanaged {
		t.Fatalf("unexpected group detail: %+v", group)
	}
}

func TestCancelSyncedOrderCancelsStandaloneOpenOrder(t *testing.T) {
	broker := newFakeBroker()
	broker.synced = []KiteOrder{
		{OrderID: "external-open", Exchange: "NSE", TradingSymbol: "INFY", TransactionType: "BUY", Quantity: 1, Product: "MIS", OrderType: "LIMIT", Status: OrderStatusOpen, Price: 100},
	}
	manager := NewManager(broker)
	if _, err := manager.SyncKite(context.Background()); err != nil {
		t.Fatal(err)
	}

	result, err := manager.CancelSyncedOrder(context.Background(), "external-open")
	if err != nil {
		t.Fatal(err)
	}
	if result.Trade != nil {
		t.Fatalf("expected no linked trade, got %+v", result.Trade)
	}
	if result.Order == nil || result.Order.Status != OrderStatusCancelled {
		t.Fatalf("expected cancelled order result, got %+v", result)
	}
	if broker.cancelCount != 1 {
		t.Fatalf("expected one broker cancel, got %d", broker.cancelCount)
	}
	order, err := manager.GetSyncedOrder("external-open")
	if err != nil {
		t.Fatal(err)
	}
	if order.Status != OrderStatusCancelled {
		t.Fatalf("expected synced order status cancelled, got %+v", order)
	}
}

func TestCancelSyncedOrderClearsLinkedStopLoss(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)
	trade, err := manager.Enter(context.Background(), CreateTradeRequest{
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		OrderType:     "MARKET",
	})
	if err != nil {
		t.Fatal(err)
	}
	trade, err = manager.AddStopLoss(context.Background(), trade.ID, StopLossRequest{TriggerPrice: 90, LimitPrice: 89})
	if err != nil {
		t.Fatal(err)
	}
	stopOrderID := trade.StopOrderID
	if _, err := manager.SyncKite(context.Background()); err != nil {
		t.Fatal(err)
	}

	result, err := manager.CancelSyncedOrder(context.Background(), stopOrderID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Trade == nil || result.Trade.StopOrderID != "" || result.Trade.StopLoss != nil {
		t.Fatalf("expected linked stop-loss cleared, got %+v", result)
	}
	if result.Order == nil || result.Order.Status != OrderStatusCancelled {
		t.Fatalf("expected synced stop order cancelled, got %+v", result)
	}
	if broker.cancelCount != 1 {
		t.Fatalf("expected one broker cancel, got %d", broker.cancelCount)
	}
}

func TestLinkAndUnlinkGroupExternalExitOrder(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)
	_, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		EntryPrice:    100,
		EntryOrderID:  "kite-entry",
	})
	if err != nil {
		t.Fatal(err)
	}
	broker.synced = []KiteOrder{
		{OrderID: "ext-sl-1", Exchange: "NSE", TradingSymbol: "INFY", TransactionType: "SELL", Quantity: 1, Product: "MIS", OrderType: "SL", Status: OrderStatusOpen, Price: 89, TriggerPrice: 90},
		{OrderID: "ext-sl-2", Exchange: "NSE", TradingSymbol: "INFY", TransactionType: "SELL", Quantity: 1, Product: "MIS", OrderType: "SL", Status: OrderStatusOpen, Price: 88, TriggerPrice: 89},
	}
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 1}}
	if _, err := manager.SyncKite(context.Background()); err != nil {
		t.Fatal(err)
	}

	trade, err := manager.LinkGroupExternalExitOrder(context.Background(), "NSE:INFY:MIS", ExternalExitLinkRequest{
		OrderID: "ext-sl-2",
		Role:    "stop_loss",
	})
	if err != nil {
		t.Fatal(err)
	}
	if trade.StopOrderID != "ext-sl-2" || trade.StopLoss == nil || trade.StopLoss.TriggerPrice != 89 || trade.StopLoss.LimitPrice != 88 {
		t.Fatalf("expected manually linked external SL, got %+v", trade)
	}

	trade, err = manager.AddGroupStopLoss(context.Background(), "NSE:INFY:MIS", StopLossRequest{TriggerPrice: 91, LimitPrice: 90})
	if err != nil {
		t.Fatal(err)
	}
	if trade.StopOrderID != "ext-sl-2" || broker.modifyCount != 1 {
		t.Fatalf("expected linked external SL to be modified, got trade=%+v modifyCount=%d", trade, broker.modifyCount)
	}

	trade, err = manager.UnlinkGroupExternalExitOrder(context.Background(), "NSE:INFY:MIS", ExternalExitLinkRequest{
		OrderID: "ext-sl-2",
		Role:    "sl",
	})
	if err != nil {
		t.Fatal(err)
	}
	if trade.StopOrderID != "" || trade.StopLoss != nil {
		t.Fatalf("expected external SL to be unlinked locally, got %+v", trade)
	}
	if broker.cancelCount != 0 {
		t.Fatalf("expected unlink to avoid broker cancellation, got %d cancels", broker.cancelCount)
	}
}

func TestLinkGroupExternalExitOrderRejectsInvalidOrder(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)
	_, err := manager.Import(context.Background(), ImportTradeRequest{
		ID:            "t1",
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		EntryPrice:    100,
		EntryOrderID:  "kite-entry",
	})
	if err != nil {
		t.Fatal(err)
	}
	broker.synced = []KiteOrder{
		{OrderID: "wrong-side-target", Exchange: "NSE", TradingSymbol: "INFY", TransactionType: "BUY", Quantity: 1, Product: "MIS", OrderType: "LIMIT", Status: OrderStatusOpen, Price: 120},
	}
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 1}}
	if _, err := manager.SyncKite(context.Background()); err != nil {
		t.Fatal(err)
	}

	_, err = manager.LinkGroupExternalExitOrder(context.Background(), "NSE:INFY:MIS", ExternalExitLinkRequest{
		OrderID: "wrong-side-target",
		Role:    "target",
	})
	var domainErr *DomainError
	if !errors.As(err, &domainErr) || domainErr.Code != "invalid_external_exit_order" {
		t.Fatalf("expected invalid_external_exit_order, got %v", err)
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

func TestExitCancelsOpenLimitEntryInsteadOfPlacingReverseMarketOrder(t *testing.T) {
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
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	trade, err = manager.Exit(context.Background(), trade.ID)
	if err != nil {
		t.Fatal(err)
	}
	if trade.TradeStatus != TradeStatusClosed || trade.ExitReason != ExitReasonEntryCancelled {
		t.Fatalf("expected entry-cancelled closed trade, got %+v", trade)
	}
	if trade.EntryStatus != OrderStatusCancelled {
		t.Fatalf("expected entry status cancelled, got %s", trade.EntryStatus)
	}
	if trade.ExitOrderID != "" {
		t.Fatalf("expected no reverse market exit order, got %s", trade.ExitOrderID)
	}
	if trade.PendingProtection != nil || trade.PendingStopLoss != nil || trade.PendingTarget != nil {
		t.Fatalf("expected pending protection cleared, got %+v", trade)
	}
	if broker.placeCount != 1 || broker.cancelCount != 1 {
		t.Fatalf("expected one entry place and one cancel, got place=%d cancel=%d", broker.placeCount, broker.cancelCount)
	}
}

func TestCancelEntryRejectsCompletedEntry(t *testing.T) {
	broker := newFakeBroker()
	manager := NewManager(broker)

	trade, err := manager.Enter(context.Background(), CreateTradeRequest{
		Exchange:      "NSE",
		TradingSymbol: "INFY",
		Side:          "BUY",
		Quantity:      1,
		Product:       "MIS",
		OrderType:     "MARKET",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = manager.CancelEntry(context.Background(), trade.ID)
	var domainErr *DomainError
	if !errors.As(err, &domainErr) || domainErr.Code != "entry_already_complete" {
		t.Fatalf("expected entry_already_complete, got %v", err)
	}
	if broker.cancelCount != 0 {
		t.Fatalf("expected no broker cancel for completed entry, got %d", broker.cancelCount)
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

func TestJSONOrderStorePersistsAndReloadsSyncedOrders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trades.json")
	broker := newFakeBroker()
	broker.synced = []KiteOrder{
		{
			OrderID:         "local-1",
			Exchange:        "NSE",
			TradingSymbol:   "INFY",
			TransactionType: "BUY",
			Quantity:        1,
			Product:         "MIS",
			OrderType:       "MARKET",
			Status:          OrderStatusComplete,
			Tag:             LocalSystemOrderTag,
		},
	}
	manager, err := NewManagerWithStores(broker, NewJSONStore(path), NewJSONOrderStore(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.SyncKite(context.Background()); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewManagerWithStores(newFakeBroker(), NewJSONStore(path), NewJSONOrderStore(path))
	if err != nil {
		t.Fatal(err)
	}
	orders := reloaded.ListSyncedOrders()
	if len(orders) != 1 {
		t.Fatalf("expected one reloaded order, got %d", len(orders))
	}
	if orders[0].OrderID != "local-1" || orders[0].CreationSource != CreationSourceLocalSystem {
		t.Fatalf("unexpected reloaded order %+v", orders[0])
	}
}

func TestJSONPositionStorePersistsAndReloadsSyncedPositions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trades.json")
	broker := newFakeBroker()
	broker.positions = []Position{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS", Quantity: 2}}
	manager, err := NewManagerWithAllStores(broker, NewJSONStore(path), NewJSONOrderStore(path), NewJSONPositionStore(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.SyncKite(context.Background()); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewManagerWithAllStores(newFakeBroker(), NewJSONStore(path), NewJSONOrderStore(path), NewJSONPositionStore(path))
	if err != nil {
		t.Fatal(err)
	}
	positions := reloaded.ListSyncedPositions()
	if len(positions) != 1 {
		t.Fatalf("expected one reloaded position, got %d", len(positions))
	}
	if positions[0].TradingSymbol != "INFY" || positions[0].Quantity != 2 || positions[0].SyncedAt.IsZero() {
		t.Fatalf("unexpected reloaded position %+v", positions[0])
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

func TestDailyOrderStorePathUsesDateWhenDirectoryProvided(t *testing.T) {
	got := dailyOrderStorePath("/tmp/trading-data", time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC))
	want := filepath.Join("/tmp/trading-data", "orders_24_05_2026.json")
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}

	explicit := "/tmp/custom.json"
	wantExplicit := "/tmp/custom_orders.json"
	if got := dailyOrderStorePath(explicit, time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)); got != wantExplicit {
		t.Fatalf("got %s want %s", got, wantExplicit)
	}
}

func TestDailyPositionStorePathUsesDateWhenDirectoryProvided(t *testing.T) {
	got := dailyPositionStorePath("/tmp/trading-data", time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC))
	want := filepath.Join("/tmp/trading-data", "positions_24_05_2026.json")
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}

	explicit := "/tmp/custom.json"
	wantExplicit := "/tmp/custom_positions.json"
	if got := dailyPositionStorePath(explicit, time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)); got != wantExplicit {
		t.Fatalf("got %s want %s", got, wantExplicit)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
