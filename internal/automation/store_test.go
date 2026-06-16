package automation

import (
	"testing"
	"time"
)

func TestJSONCandleStoreSaveMergesByTime(t *testing.T) {
	store := NewJSONCandleStore(t.TempDir())
	req := HistoricalRequest{
		Exchange:        "MCX",
		TradingSymbol:   "SILVERM26JUNFUT",
		InstrumentToken: 118822663,
		Interval:        "minute",
		From:            time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		To:              time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
	}
	candleTime := time.Date(2026, 6, 1, 9, 15, 0, 0, time.UTC)
	if _, _, err := store.Save(req, []Candle{{Time: candleTime, Open: 1, High: 2, Low: 1, Close: 2, Volume: 10}}); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	if _, stored, err := store.Save(req, []Candle{{Time: candleTime, Open: 2, High: 3, Low: 2, Close: 3, Volume: 20}}); err != nil {
		t.Fatalf("save failed: %v", err)
	} else if stored != 1 {
		t.Fatalf("expected one merged candle, got %d", stored)
	}
	loaded, err := store.Load(BacktestRequest{
		Exchange:      req.Exchange,
		TradingSymbol: req.TradingSymbol,
		Interval:      req.Interval,
		From:          req.From,
		To:            req.To,
	})
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(loaded) != 1 || loaded[0].Close != 3 || loaded[0].Volume != 20 {
		t.Fatalf("unexpected loaded candles: %+v", loaded)
	}
}

func TestJSONCandleStoreSavePartitionsByMonth(t *testing.T) {
	store := NewJSONCandleStore(t.TempDir())
	req := HistoricalRequest{
		Exchange:        "MCX",
		TradingSymbol:   "SILVERM26JUNFUT",
		InstrumentToken: 118822663,
		Interval:        "minute",
		From:            time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC),
		To:              time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
	}
	paths, _, err := store.Save(req, []Candle{
		{Time: time.Date(2026, 6, 30, 9, 15, 0, 0, time.UTC), Open: 1},
		{Time: time.Date(2026, 7, 1, 9, 15, 0, 0, time.UTC), Open: 2},
	})
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected two monthly paths, got %+v", paths)
	}
}
