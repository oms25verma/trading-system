package automation

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type CandleStore interface {
	Load(req BacktestRequest) ([]Candle, error)
	Save(req HistoricalRequest, candles []Candle) ([]string, int, error)
	List(req CandleListRequest) ([]Candle, error)
}

type BacktestStore interface {
	Save(result *BacktestResult) (string, error)
	List() ([]BacktestSummary, error)
	Get(id string) (*BacktestResult, error)
}

type PaperRunStore interface {
	Save(result *PaperRunResult) (string, error)
	List() ([]PaperRunSummary, error)
	Get(id string) (*PaperRunResult, error)
}

type CandleListRequest struct {
	Exchange      string
	TradingSymbol string
	Interval      string
	From          time.Time
	To            time.Time
}

type JSONCandleStore struct {
	baseDir string
}

type JSONBacktestStore struct {
	baseDir string
}

type JSONPaperRunStore struct {
	baseDir string
}

func NewJSONCandleStore(baseDir string) *JSONCandleStore {
	if strings.TrimSpace(baseDir) == "" {
		baseDir = "data"
	}
	return &JSONCandleStore{baseDir: baseDir}
}

func NewJSONBacktestStore(baseDir string) *JSONBacktestStore {
	if strings.TrimSpace(baseDir) == "" {
		baseDir = "data"
	}
	return &JSONBacktestStore{baseDir: baseDir}
}

func NewJSONPaperRunStore(baseDir string) *JSONPaperRunStore {
	if strings.TrimSpace(baseDir) == "" {
		baseDir = "data"
	}
	return &JSONPaperRunStore{baseDir: baseDir}
}

func (s *JSONCandleStore) Save(req HistoricalRequest, candles []Candle) ([]string, int, error) {
	byPath := make(map[string][]Candle)
	for _, candle := range candles {
		path := s.path(req.Exchange, req.TradingSymbol, req.Interval, candle.Time)
		byPath[path] = append(byPath[path], candle)
	}
	paths := make([]string, 0, len(byPath))
	stored := 0
	for path, monthCandles := range byPath {
		count, err := s.saveFile(path, monthCandles)
		if err != nil {
			return nil, 0, err
		}
		paths = append(paths, path)
		stored += count
	}
	sort.Strings(paths)
	return paths, stored, nil
}

func (s *JSONCandleStore) Load(req BacktestRequest) ([]Candle, error) {
	return s.List(CandleListRequest{
		Exchange:      req.Exchange,
		TradingSymbol: req.TradingSymbol,
		Interval:      req.Interval,
		From:          req.From,
		To:            req.To,
	})
}

func (s *JSONCandleStore) List(req CandleListRequest) ([]Candle, error) {
	var out []Candle
	for month := monthStart(req.From); !month.After(req.To); month = month.AddDate(0, 1, 0) {
		path := s.path(req.Exchange, req.TradingSymbol, req.Interval, month)
		candles, err := s.loadFile(path)
		if err != nil {
			return nil, err
		}
		for _, candle := range candles {
			if candle.Time.Before(req.From) || candle.Time.After(req.To) {
				continue
			}
			out = append(out, candle)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out, nil
}

func (s *JSONCandleStore) saveFile(path string, candles []Candle) (int, error) {
	existing, err := s.loadFile(path)
	if err != nil {
		return 0, err
	}
	merged := mergeCandles(existing, candles)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, err
	}
	payload, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return 0, err
	}
	payload = append(payload, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return 0, err
	}
	return len(merged), nil
}

func (s *JSONCandleStore) loadFile(path string) ([]Candle, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if len(payload) == 0 {
		return nil, nil
	}
	var candles []Candle
	if err := json.Unmarshal(payload, &candles); err != nil {
		return nil, err
	}
	return candles, nil
}

func (s *JSONCandleStore) path(exchange, symbol, interval string, at time.Time) string {
	exchange = safePart(strings.ToUpper(strings.TrimSpace(exchange)))
	symbol = safePart(strings.ToUpper(strings.TrimSpace(symbol)))
	interval = safePart(strings.ToLower(strings.TrimSpace(interval)))
	month := at.Format("2006_01")
	return filepath.Join(s.baseDir, "historical", exchange, symbol, interval+"_"+month+".json")
}

func (s *JSONBacktestStore) Save(result *BacktestResult) (string, error) {
	if result == nil {
		return "", fmt.Errorf("backtest result is required")
	}
	if result.ID == "" {
		result.ID = backtestID(result)
	}
	dir := filepath.Join(s.baseDir, "backtests")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, safePart(result.ID)+".json")
	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", err
	}
	payload = append(payload, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return "", err
	}
	return path, os.Rename(tmp, path)
}

func (s *JSONBacktestStore) List() ([]BacktestSummary, error) {
	pattern := filepath.Join(s.baseDir, "backtests", "*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	out := make([]BacktestSummary, 0, len(matches))
	for _, match := range matches {
		result, err := s.loadResult(match)
		if err != nil {
			return nil, err
		}
		out = append(out, summarizeBacktest(result))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GeneratedAt.After(out[j].GeneratedAt) })
	return out, nil
}

func (s *JSONBacktestStore) Get(id string) (*BacktestResult, error) {
	id = safePart(id)
	if id == "" || id == "UNKNOWN" {
		return nil, fmt.Errorf("backtest id is required")
	}
	return s.loadResult(filepath.Join(s.baseDir, "backtests", id+".json"))
}

func (s *JSONBacktestStore) loadResult(path string) (*BacktestResult, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result BacktestResult
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *JSONPaperRunStore) Save(result *PaperRunResult) (string, error) {
	if result == nil {
		return "", fmt.Errorf("paper run result is required")
	}
	if result.ID == "" {
		result.ID = paperRunID(result)
	}
	dir := filepath.Join(s.baseDir, "paper_runs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, safePart(result.ID)+".json")
	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", err
	}
	payload = append(payload, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return "", err
	}
	return path, os.Rename(tmp, path)
}

func (s *JSONPaperRunStore) List() ([]PaperRunSummary, error) {
	pattern := filepath.Join(s.baseDir, "paper_runs", "*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	out := make([]PaperRunSummary, 0, len(matches))
	for _, match := range matches {
		result, err := s.loadResult(match)
		if err != nil {
			return nil, err
		}
		out = append(out, summarizePaperRun(result))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GeneratedAt.After(out[j].GeneratedAt) })
	return out, nil
}

func (s *JSONPaperRunStore) Get(id string) (*PaperRunResult, error) {
	id = safePart(id)
	if id == "" || id == "UNKNOWN" {
		return nil, fmt.Errorf("paper run id is required")
	}
	return s.loadResult(filepath.Join(s.baseDir, "paper_runs", id+".json"))
}

func (s *JSONPaperRunStore) loadResult(path string) (*PaperRunResult, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result PaperRunResult
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func mergeCandles(existing, incoming []Candle) []Candle {
	byTime := make(map[time.Time]Candle, len(existing)+len(incoming))
	for _, candle := range existing {
		byTime[candle.Time.UTC()] = normalizeCandle(candle)
	}
	for _, candle := range incoming {
		byTime[candle.Time.UTC()] = normalizeCandle(candle)
	}
	out := make([]Candle, 0, len(byTime))
	for _, candle := range byTime {
		out = append(out, candle)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}

func normalizeCandle(c Candle) Candle {
	c.Time = c.Time.UTC()
	return c
}

func monthStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func safePart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "UNKNOWN"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
	return replacer.Replace(value)
}

func validateStoreRequest(exchange, symbol, interval string) error {
	if strings.TrimSpace(exchange) == "" {
		return fmt.Errorf("exchange is required")
	}
	if strings.TrimSpace(symbol) == "" {
		return fmt.Errorf("tradingsymbol is required")
	}
	if strings.TrimSpace(interval) == "" {
		return fmt.Errorf("interval is required")
	}
	return nil
}
