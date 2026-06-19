package automation

import (
	"testing"
	"time"
)

func TestPaperRunReady(t *testing.T) {
	store := paperTestStore(t)
	result, err := RunPaper(store, PaperRunRequest{
		BacktestRequest: paperBacktestRequest(),
		Guardrails: Guardrails{
			MandatoryProtection: true,
			MaxTradesPerDay:     1,
			MaxDailyLoss:        1000,
			NoEntryAfter:        "10:00",
		},
	})
	if err != nil {
		t.Fatalf("paper run failed: %v", err)
	}
	if result.Status != PaperRunStatusReady || len(result.Violations) != 0 || result.Backtest == nil {
		t.Fatalf("unexpected paper result: %+v", result)
	}
}

func TestPaperRunBlockedByKillSwitch(t *testing.T) {
	result, err := RunPaper(NewJSONCandleStore(t.TempDir()), PaperRunRequest{
		BacktestRequest: paperBacktestRequest(),
		Guardrails: Guardrails{
			MandatoryProtection: true,
			KillSwitch:          true,
		},
	})
	if err != nil {
		t.Fatalf("paper run failed: %v", err)
	}
	if result.Status != PaperRunStatusBlocked || len(result.Violations) != 1 || result.Backtest != nil {
		t.Fatalf("unexpected paper result: %+v", result)
	}
}

func TestPaperRunDetectsResultViolations(t *testing.T) {
	store := paperTestStore(t)
	result, err := RunPaper(store, PaperRunRequest{
		BacktestRequest: paperBacktestRequest(),
		Guardrails: Guardrails{
			MandatoryProtection: true,
			MaxTradesPerDay:     0,
			MaxDailyLoss:        10,
			NoEntryAfter:        "09:30",
		},
	})
	if err != nil {
		t.Fatalf("paper run failed: %v", err)
	}
	if result.Status != PaperRunStatusViolation || len(result.Violations) != 1 {
		t.Fatalf("unexpected paper result: %+v", result)
	}
	if result.Violations[0].Code != "entry_after_cutoff" {
		t.Fatalf("unexpected violation: %+v", result.Violations)
	}
}

func paperTestStore(t *testing.T) *JSONCandleStore {
	t.Helper()
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
	return store
}

func paperBacktestRequest() BacktestRequest {
	return BacktestRequest{
		Exchange:      "MCX",
		TradingSymbol: "SILVERM26JUNFUT",
		Interval:      "minute",
		From:          istTime(2026, 6, 17, 9, 15),
		To:            istTime(2026, 6, 17, 15, 30),
		Strategy:      "opening_range_breakout",
		Quantity:      1,
		Multiplier:    5,
		StopLoss:      2,
		Target:        5,
		RangeStart:    "09:15",
		RangeEnd:      "09:30",
		ExitTime:      "15:20",
	}
}

func TestPaperRunStoreRoundTrip(t *testing.T) {
	store := NewJSONPaperRunStore(t.TempDir())
	result := &PaperRunResult{
		ID:          "paper-test",
		Status:      PaperRunStatusReady,
		Request:     PaperRunRequest{BacktestRequest: paperBacktestRequest()},
		GeneratedAt: time.Now().UTC(),
		Backtest:    &BacktestResult{TotalPnL: 10, Trades: []BacktestTrade{{PnL: 10}}},
	}
	if _, err := store.Save(result); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	loaded, err := store.Get(result.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if loaded.ID != result.ID || loaded.Status != PaperRunStatusReady {
		t.Fatalf("unexpected loaded result: %+v", loaded)
	}
	summaries, err := store.List()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(summaries) != 1 || summaries[0].Trades != 1 || summaries[0].TotalPnL != 10 {
		t.Fatalf("unexpected summaries: %+v", summaries)
	}
}
