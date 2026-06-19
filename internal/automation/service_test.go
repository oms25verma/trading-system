package automation

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeHistoricalSource struct {
	called bool
}

func (f *fakeHistoricalSource) FetchHistorical(context.Context, HistoricalRequest) ([]Candle, error) {
	f.called = true
	return []Candle{{Time: time.Now().UTC(), Open: 1, High: 1, Low: 1, Close: 1}}, nil
}

type fakeHistoricalValidator struct {
	err error
}

func (f fakeHistoricalValidator) ValidateHistoricalInstrument(string, string, int64) error {
	return f.err
}

func TestSyncHistoricalStopsBeforeFetchWhenInstrumentValidationFails(t *testing.T) {
	source := &fakeHistoricalSource{}
	service := NewServiceWithValidator(source, NewJSONCandleStore(t.TempDir()), fakeHistoricalValidator{err: errors.New("invalid instrument")})
	_, err := service.SyncHistorical(context.Background(), HistoricalRequest{
		Exchange:        "NSE",
		TradingSymbol:   "SILVERM26JUNFUT",
		InstrumentToken: 118822663,
		Interval:        "minute",
		From:            time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		To:              time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if source.called {
		t.Fatal("source should not be called when instrument validation fails")
	}
}
