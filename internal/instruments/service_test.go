package instruments

import (
	"context"
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
