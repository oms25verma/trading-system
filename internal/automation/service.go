package automation

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type HistoricalSource interface {
	FetchHistorical(ctx context.Context, req HistoricalRequest) ([]Candle, error)
}

type HistoricalInstrumentValidator interface {
	ValidateHistoricalInstrument(exchange, tradingSymbol string, instrumentToken int64) error
}

type Service struct {
	source        HistoricalSource
	store         CandleStore
	backtestStore BacktestStore
	paperRunStore PaperRunStore
	validator     HistoricalInstrumentValidator
}

func NewService(source HistoricalSource, store CandleStore) *Service {
	return &Service{source: source, store: store}
}

func NewServiceWithBacktests(source HistoricalSource, store CandleStore, backtestStore BacktestStore) *Service {
	return &Service{source: source, store: store, backtestStore: backtestStore}
}

func NewServiceWithStores(source HistoricalSource, store CandleStore, backtestStore BacktestStore, paperRunStore PaperRunStore) *Service {
	return &Service{source: source, store: store, backtestStore: backtestStore, paperRunStore: paperRunStore}
}

func NewServiceWithValidator(source HistoricalSource, store CandleStore, validator HistoricalInstrumentValidator) *Service {
	return &Service{source: source, store: store, validator: validator}
}

func (s *Service) SetHistoricalInstrumentValidator(validator HistoricalInstrumentValidator) {
	s.validator = validator
}

func (s *Service) SyncHistorical(ctx context.Context, req HistoricalRequest) (*HistoricalSyncResult, error) {
	if s.source == nil {
		return nil, fmt.Errorf("historical source is not configured")
	}
	if s.store == nil {
		return nil, fmt.Errorf("historical store is not configured")
	}
	req = normalizeHistoricalRequest(req)
	if err := validateHistoricalRequest(req); err != nil {
		return nil, err
	}
	if s.validator != nil {
		if err := s.validator.ValidateHistoricalInstrument(req.Exchange, req.TradingSymbol, req.InstrumentToken); err != nil {
			return nil, err
		}
	}
	candles, err := s.source.FetchHistorical(ctx, req)
	if err != nil {
		return nil, err
	}
	paths, stored, err := s.store.Save(req, candles)
	if err != nil {
		return nil, err
	}
	path := ""
	if len(paths) > 0 {
		path = paths[0]
	}
	return &HistoricalSyncResult{
		Exchange:        req.Exchange,
		TradingSymbol:   req.TradingSymbol,
		InstrumentToken: req.InstrumentToken,
		Interval:        req.Interval,
		From:            req.From,
		To:              req.To,
		CandlesFetched:  len(candles),
		CandlesStored:   stored,
		Path:            path,
		Paths:           paths,
		SyncedAt:        time.Now().UTC(),
	}, nil
}

func (s *Service) ListCandles(req CandleListRequest) ([]Candle, error) {
	if s.store == nil {
		return nil, fmt.Errorf("historical store is not configured")
	}
	req.Exchange = strings.ToUpper(strings.TrimSpace(req.Exchange))
	req.TradingSymbol = strings.ToUpper(strings.TrimSpace(req.TradingSymbol))
	req.Interval = strings.ToLower(strings.TrimSpace(req.Interval))
	req.From = req.From.UTC()
	req.To = req.To.UTC()
	if err := validateStoreRequest(req.Exchange, req.TradingSymbol, req.Interval); err != nil {
		return nil, err
	}
	if req.From.IsZero() || req.To.IsZero() || !req.From.Before(req.To) {
		return nil, fmt.Errorf("valid from/to range is required")
	}
	return s.store.List(req)
}

func (s *Service) RunBacktest(req BacktestRequest) (*BacktestResult, error) {
	backtester := NewBacktester(s.store)
	result, err := backtester.Run(req)
	if err != nil {
		return nil, err
	}
	if s.backtestStore != nil {
		if _, err := s.backtestStore.Save(result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *Service) ListBacktests() ([]BacktestSummary, error) {
	if s.backtestStore == nil {
		return nil, fmt.Errorf("backtest store is not configured")
	}
	return s.backtestStore.List()
}

func (s *Service) GetBacktest(id string) (*BacktestResult, error) {
	if s.backtestStore == nil {
		return nil, fmt.Errorf("backtest store is not configured")
	}
	return s.backtestStore.Get(id)
}

func (s *Service) RunPaper(req PaperRunRequest) (*PaperRunResult, error) {
	result, err := RunPaper(s.store, req)
	if err != nil {
		return nil, err
	}
	if s.paperRunStore != nil {
		if _, err := s.paperRunStore.Save(result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *Service) ListPaperRuns() ([]PaperRunSummary, error) {
	if s.paperRunStore == nil {
		return nil, fmt.Errorf("paper run store is not configured")
	}
	return s.paperRunStore.List()
}

func (s *Service) GetPaperRun(id string) (*PaperRunResult, error) {
	if s.paperRunStore == nil {
		return nil, fmt.Errorf("paper run store is not configured")
	}
	return s.paperRunStore.Get(id)
}

func normalizeHistoricalRequest(req HistoricalRequest) HistoricalRequest {
	req.Exchange = strings.ToUpper(strings.TrimSpace(req.Exchange))
	req.TradingSymbol = strings.ToUpper(strings.TrimSpace(req.TradingSymbol))
	req.Interval = strings.ToLower(strings.TrimSpace(req.Interval))
	req.From = req.From.UTC()
	req.To = req.To.UTC()
	return req
}

func validateHistoricalRequest(req HistoricalRequest) error {
	if err := validateStoreRequest(req.Exchange, req.TradingSymbol, req.Interval); err != nil {
		return err
	}
	if req.InstrumentToken <= 0 {
		return fmt.Errorf("instrument_token is required")
	}
	if req.From.IsZero() || req.To.IsZero() {
		return fmt.Errorf("from and to are required")
	}
	if !req.From.Before(req.To) {
		return fmt.Errorf("from must be before to")
	}
	if !validInterval(req.Interval) {
		return fmt.Errorf("unsupported interval %q", req.Interval)
	}
	return nil
}

func validInterval(interval string) bool {
	switch interval {
	case "minute", "3minute", "5minute", "10minute", "15minute", "30minute", "60minute", "day":
		return true
	default:
		return false
	}
}
