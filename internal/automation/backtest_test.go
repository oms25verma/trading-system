package automation

import (
	"testing"
	"time"
)

func TestOpeningRangeBreakoutBacktest(t *testing.T) {
	store := NewJSONCandleStore(t.TempDir())
	req := HistoricalRequest{
		Exchange:        "MCX",
		TradingSymbol:   "SILVERM26JUNFUT",
		InstrumentToken: 118822663,
		Interval:        "minute",
		From:            istTime(2026, 6, 17, 9, 15),
		To:              istTime(2026, 6, 17, 15, 30),
	}
	_, _, err := store.Save(req, []Candle{
		{Time: istTime(2026, 6, 17, 9, 15), Open: 100, High: 102, Low: 99, Close: 101},
		{Time: istTime(2026, 6, 17, 9, 30), Open: 101, High: 103, Low: 100, Close: 102},
		{Time: istTime(2026, 6, 17, 9, 31), Open: 102, High: 104, Low: 101, Close: 103},
		{Time: istTime(2026, 6, 17, 9, 32), Open: 103, High: 109, Low: 103, Close: 108},
	})
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}
	result, err := NewBacktester(store).Run(BacktestRequest{
		Exchange:      "MCX",
		TradingSymbol: "SILVERM26JUNFUT",
		Interval:      "minute",
		From:          req.From,
		To:            req.To,
		Strategy:      "opening_range_breakout",
		Quantity:      1,
		Multiplier:    5,
		StopLoss:      2,
		Target:        5,
		RangeStart:    "09:15",
		RangeEnd:      "09:30",
		ExitTime:      "15:20",
	})
	if err != nil {
		t.Fatalf("backtest failed: %v", err)
	}
	if len(result.Trades) != 1 {
		t.Fatalf("expected one trade, got %+v", result.Trades)
	}
	if result.Trades[0].Reason != "TARGET" || result.TotalPnL != 25 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.GrossPnL != 25 || result.TotalCosts != 0 || result.Expectancy != 25 || len(result.EquityCurve) != 1 {
		t.Fatalf("unexpected metrics: %+v", result)
	}
}

func TestOpeningRangeBreakoutBacktestAppliesSlippageAndBrokerage(t *testing.T) {
	store := NewJSONCandleStore(t.TempDir())
	req := HistoricalRequest{
		Exchange:        "NFO",
		TradingSymbol:   "NIFTY2661623250CE",
		InstrumentToken: 12345,
		Interval:        "minute",
		From:            istTime(2026, 6, 17, 9, 15),
		To:              istTime(2026, 6, 17, 15, 30),
	}
	_, _, err := store.Save(req, []Candle{
		{Time: istTime(2026, 6, 17, 9, 15), Open: 100, High: 102, Low: 99, Close: 101},
		{Time: istTime(2026, 6, 17, 9, 30), Open: 101, High: 103, Low: 100, Close: 102},
		{Time: istTime(2026, 6, 17, 9, 31), Open: 102, High: 110, Low: 101, Close: 109},
		{Time: istTime(2026, 6, 17, 9, 32), Open: 109, High: 112, Low: 108, Close: 111},
	})
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}
	result, err := NewBacktester(store).Run(BacktestRequest{
		Exchange:      "NFO",
		TradingSymbol: "NIFTY2661623250CE",
		Interval:      "minute",
		From:          req.From,
		To:            req.To,
		Strategy:      "opening_range_breakout",
		Quantity:      65,
		Multiplier:    1,
		StopLoss:      2,
		Target:        5,
		Slippage:      0.5,
		Brokerage:     40,
		RangeStart:    "09:15",
		RangeEnd:      "09:30",
		ExitTime:      "15:20",
	})
	if err != nil {
		t.Fatalf("backtest failed: %v", err)
	}
	if len(result.Trades) != 1 {
		t.Fatalf("expected one trade, got %+v", result.Trades)
	}
	trade := result.Trades[0]
	if trade.Entry != 103.5 || trade.Exit != 108 {
		t.Fatalf("unexpected slippage-adjusted prices: %+v", trade)
	}
	if trade.GrossPnL != 292.5 || trade.Costs != 40 || trade.PnL != 252.5 {
		t.Fatalf("unexpected pnl after costs: %+v", trade)
	}
	if result.TotalPnL != 252.5 || result.GrossPnL != 292.5 || result.TotalCosts != 40 || result.Expectancy != 252.5 {
		t.Fatalf("unexpected result metrics: %+v", result)
	}
}

func istTime(year int, month time.Month, day, hour, minute int) time.Time {
	location := time.FixedZone("IST", 5*60*60+30*60)
	return time.Date(year, month, day, hour, minute, 0, 0, location).UTC()
}
