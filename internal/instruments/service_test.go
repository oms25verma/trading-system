package instruments

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"trading-system/internal/kite"
)

type fakeKiteClient struct {
	items []kite.Instrument
	ltp   float64
	err   error
}

func (f fakeKiteClient) Instruments(context.Context, string) ([]kite.Instrument, error) {
	return f.items, f.err
}

func (f fakeKiteClient) LTP(context.Context, string, string) (float64, error) {
	return f.ltp, f.err
}

func TestOptionsFiltersByExpiryAndRange(t *testing.T) {
	service := NewService(fakeKiteClient{items: niftyOptions(), ltp: 23520}, t.TempDir())
	service.now = func() time.Time { return time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC) }
	if _, err := service.Sync(context.Background(), "NFO"); err != nil {
		t.Fatal(err)
	}

	result, err := service.Options(context.Background(), OptionFilter{
		Exchange:          "NFO",
		Underlying:        "NIFTY",
		Expiry:            "2026-06-16",
		RangePoints:       100,
		ContractsEachSide: 1,
		Product:           "MIS",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.CenterStrike != 23500 || result.CenterSource != "ltp" {
		t.Fatalf("unexpected center: %+v", result)
	}
	if len(result.Contracts) != 6 {
		t.Fatalf("expected 3 strikes x CE/PE, got %d: %+v", len(result.Contracts), result.Contracts)
	}
	for _, contract := range result.Contracts {
		if contract.Expiry != "2026-06-16" || contract.Underlying != "NIFTY" || contract.Product != "MIS" {
			t.Fatalf("unexpected contract: %+v", contract)
		}
	}
}

func TestExpiriesAreSorted(t *testing.T) {
	service := NewService(fakeKiteClient{items: niftyOptions()}, t.TempDir())
	service.now = func() time.Time { return time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC) }
	if _, err := service.Sync(context.Background(), "NFO"); err != nil {
		t.Fatal(err)
	}

	expiries, err := service.Expiries("NFO", "NIFTY")
	if err != nil {
		t.Fatal(err)
	}
	if len(expiries) != 2 || expiries[0] != "2026-06-16" || expiries[1] != "2026-06-23" {
		t.Fatalf("unexpected expiries: %+v", expiries)
	}
}

func TestFuturesListsMCXContractsByUnderlyingAndExpiry(t *testing.T) {
	service := NewService(fakeKiteClient{items: mcxFutures()}, t.TempDir())
	service.now = func() time.Time { return time.Date(2026, 6, 13, 9, 0, 0, 0, time.UTC) }
	if _, err := service.Sync(context.Background(), "MCX"); err != nil {
		t.Fatal(err)
	}

	underlyings, err := service.FutureUnderlyings("MCX")
	if err != nil {
		t.Fatal(err)
	}
	if len(underlyings) != 3 || underlyings[0] != "GOLD" || underlyings[1] != "SILVER" || underlyings[2] != "SILVERM" {
		t.Fatalf("unexpected underlyings: %+v", underlyings)
	}

	result, err := service.Futures("MCX", "SILVERM", "MIS")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Contracts) != 2 {
		t.Fatalf("expected two SILVERM contracts, got %+v", result.Contracts)
	}
	if result.Contracts[0].TradingSymbol != "SILVERM26JUNFUT" || result.Contracts[1].TradingSymbol != "SILVERM26JULFUT" {
		t.Fatalf("expected nearest expiry first, got %+v", result.Contracts)
	}
	if result.Contracts[0].LotSize != 1 || result.Contracts[0].Product != "MIS" {
		t.Fatalf("unexpected contract metadata: %+v", result.Contracts[0])
	}
}

func TestHistoricalInstrumentRegistryPreservesExpiredContracts(t *testing.T) {
	dir := t.TempDir()
	service := NewService(fakeKiteClient{items: mcxFutures()}, dir)
	service.now = func() time.Time { return time.Date(2026, 6, 13, 9, 0, 0, 0, time.UTC) }
	if _, err := service.Sync(context.Background(), "MCX"); err != nil {
		t.Fatal(err)
	}

	service.client = fakeKiteClient{items: []kite.Instrument{
		{
			InstrumentToken: 999,
			TradingSymbol:   "SILVERM26AUGFUT",
			Name:            "SILVERM",
			Expiry:          "2026-08-31",
			TickSize:        1,
			LotSize:         1,
			InstrumentType:  "FUT",
			Segment:         "MCX-FUT",
			Exchange:        "MCX",
		},
	}}
	service.now = func() time.Time { return time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC) }
	if _, err := service.Sync(context.Background(), "MCX"); err != nil {
		t.Fatal(err)
	}

	result, err := service.HistoricalInstruments(HistoricalInstrumentFilter{
		Exchange:      "MCX",
		TradingSymbol: "SILVERM26JUNFUT",
		Limit:         10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("expected preserved June contract, got %+v", result)
	}
	if result[0].InstrumentToken != 118822663 || result[0].FirstSeen != "2026-06-13" || result[0].LastSeen != "2026-06-13" {
		t.Fatalf("unexpected preserved contract: %+v", result[0])
	}

	latest, err := service.HistoricalInstruments(HistoricalInstrumentFilter{
		Exchange:       "MCX",
		Underlying:     "SILVERM",
		InstrumentType: "FUT",
		Limit:          10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(latest) != 3 {
		t.Fatalf("expected registry to include old and new silverm contracts, got %+v", latest)
	}
}

func TestHistoricalInstrumentsFallsBackToLatestCache(t *testing.T) {
	service := NewService(fakeKiteClient{items: mcxFutures()}, t.TempDir())
	service.now = func() time.Time { return time.Date(2026, 6, 13, 9, 0, 0, 0, time.UTC) }
	items := mcxFutures()
	path := service.cachePath("MCX", service.now())
	if err := os.MkdirAll(service.baseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	payload, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := service.HistoricalInstruments(HistoricalInstrumentFilter{
		Exchange:      "MCX",
		TradingSymbol: "SILVERM26JUNFUT",
		Limit:         10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].InstrumentToken != 118822663 {
		t.Fatalf("unexpected fallback result: %+v", result)
	}
}

func TestValidateHistoricalInstrumentRejectsExchangeSymbolTokenMismatch(t *testing.T) {
	service := NewService(fakeKiteClient{items: mcxFutures()}, t.TempDir())
	service.now = func() time.Time { return time.Date(2026, 6, 13, 9, 0, 0, 0, time.UTC) }
	if _, err := service.Sync(context.Background(), "MCX"); err != nil {
		t.Fatal(err)
	}

	if err := service.ValidateHistoricalInstrument("MCX", "SILVERM26JUNFUT", 118822663); err != nil {
		t.Fatalf("expected valid instrument, got %v", err)
	}
	if err := service.ValidateHistoricalInstrument("NSE", "SILVERM26JUNFUT", 118822663); err == nil {
		t.Fatal("expected exchange mismatch error")
	}
	if err := service.ValidateHistoricalInstrument("MCX", "SILVERM26JUNFUT", 218822663); err == nil {
		t.Fatal("expected token mismatch error")
	}
}

func TestCacheStatusesReportMissingAndSyncedExchanges(t *testing.T) {
	service := NewService(fakeKiteClient{items: mcxFutures()}, t.TempDir())
	service.now = func() time.Time { return time.Date(2026, 6, 13, 9, 0, 0, 0, time.UTC) }
	if _, err := service.Sync(context.Background(), "MCX"); err != nil {
		t.Fatal(err)
	}

	statuses, err := service.CacheStatuses([]string{"MCX", "NSE"})
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 {
		t.Fatalf("unexpected statuses: %+v", statuses)
	}
	if !statuses[0].Cached || statuses[0].Count != 4 || statuses[0].RegistryCount != 4 {
		t.Fatalf("unexpected MCX status: %+v", statuses[0])
	}
	if statuses[1].Cached || statuses[1].RegistryCount != 0 {
		t.Fatalf("unexpected NSE status: %+v", statuses[1])
	}
}

func TestHistoricalUnderlyingsFromRegistry(t *testing.T) {
	service := NewService(fakeKiteClient{items: mcxFutures()}, t.TempDir())
	service.now = func() time.Time { return time.Date(2026, 6, 13, 9, 0, 0, 0, time.UTC) }
	if _, err := service.Sync(context.Background(), "MCX"); err != nil {
		t.Fatal(err)
	}

	result, err := service.HistoricalUnderlyings(HistoricalUnderlyingFilter{Exchange: "MCX", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 || result[0].Underlying != "SILVERM" || result[1].Underlying != "GOLD" || result[2].Underlying != "SILVER" {
		t.Fatalf("unexpected underlyings: %+v", result)
	}
	if result[0].Count != 2 || len(result[0].InstrumentTypes) != 1 || result[0].InstrumentTypes[0] != "FUT" {
		t.Fatalf("unexpected SILVERM aggregate: %+v", result[0])
	}

	filtered, err := service.HistoricalUnderlyings(HistoricalUnderlyingFilter{Exchange: "MCX", Query: "SILVERM", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].Underlying != "SILVERM" {
		t.Fatalf("unexpected filtered underlyings: %+v", filtered)
	}
}

func TestHistoricalUnderlyingsMergeRegistryWithLatestCache(t *testing.T) {
	service := NewService(fakeKiteClient{items: mcxFutures()}, t.TempDir())
	service.now = func() time.Time { return time.Date(2026, 6, 13, 9, 0, 0, 0, time.UTC) }
	if _, err := service.Sync(context.Background(), "MCX"); err != nil {
		t.Fatal(err)
	}

	path := service.cachePath("NFO", service.now())
	payload, err := json.MarshalIndent(niftyOptions(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := service.HistoricalUnderlyings(HistoricalUnderlyingFilter{Exchange: "NFO", Query: "NIFTY", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].Underlying != "NIFTY" {
		t.Fatalf("expected NIFTY from latest cache even with registry present, got %+v", result)
	}
}

func niftyOptions() []kite.Instrument {
	items := make([]kite.Instrument, 0)
	for _, expiry := range []string{"2026-06-23", "2026-06-16"} {
		for _, strike := range []float64{23400, 23500, 23600, 23700} {
			for _, typ := range []string{"CE", "PE"} {
				items = append(items, kite.Instrument{
					TradingSymbol:  "NIFTY26" + typ,
					Name:           "NIFTY",
					Expiry:         expiry,
					Strike:         strike,
					TickSize:       0.05,
					LotSize:        75,
					InstrumentType: typ,
					Segment:        "NFO-OPT",
					Exchange:       "NFO",
				})
			}
		}
	}
	return items
}

func mcxFutures() []kite.Instrument {
	return []kite.Instrument{
		{
			InstrumentToken: 118822664,
			TradingSymbol:   "SILVERM26JULFUT",
			Name:            "SILVERM",
			Expiry:          "2026-07-31",
			TickSize:        1,
			LotSize:         1,
			InstrumentType:  "FUT",
			Segment:         "MCX-FUT",
			Exchange:        "MCX",
		},
		{
			InstrumentToken: 218822663,
			TradingSymbol:   "GOLD26JUNFUT",
			Name:            "GOLD",
			Expiry:          "2026-06-30",
			TickSize:        1,
			LotSize:         1,
			InstrumentType:  "FUT",
			Segment:         "MCX-FUT",
			Exchange:        "MCX",
		},
		{
			InstrumentToken: 118822663,
			TradingSymbol:   "SILVERM26JUNFUT",
			Name:            "SILVERM",
			Expiry:          "2026-06-30",
			TickSize:        1,
			LotSize:         1,
			InstrumentType:  "FUT",
			Segment:         "MCX-FUT",
			Exchange:        "MCX",
		},
		{
			InstrumentToken: 318822663,
			TradingSymbol:   "SILVER26JUNFUT",
			Name:            "SILVER",
			Expiry:          "2026-06-30",
			TickSize:        1,
			LotSize:         1,
			InstrumentType:  "FUT",
			Segment:         "MCX-FUT",
			Exchange:        "MCX",
		},
	}
}
