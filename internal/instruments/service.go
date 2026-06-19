package instruments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"trading-system/internal/kite"
)

var ErrInstrumentCacheMissing = errors.New("instrument cache missing")

type KiteClient interface {
	Instruments(ctx context.Context, exchange string) ([]kite.Instrument, error)
	LTP(ctx context.Context, exchange, symbol string) (float64, error)
}

type Service struct {
	client  KiteClient
	baseDir string
	now     func() time.Time
}

type SyncResult struct {
	Exchange      string    `json:"exchange"`
	Count         int       `json:"count"`
	Path          string    `json:"path"`
	RegistryPath  string    `json:"registry_path,omitempty"`
	RegistryCount int       `json:"registry_count,omitempty"`
	SyncedAt      time.Time `json:"synced_at"`
	ExpiresAt     time.Time `json:"expires_at,omitempty"`
}

type OptionContract struct {
	Exchange        string  `json:"exchange"`
	TradingSymbol   string  `json:"tradingsymbol"`
	Underlying      string  `json:"underlying"`
	Expiry          string  `json:"expiry"`
	Strike          float64 `json:"strike"`
	InstrumentType  string  `json:"instrument_type"`
	Product         string  `json:"product"`
	DefaultQuantity int     `json:"default_quantity"`
	LotSize         int     `json:"lot_size"`
	TickSize        float64 `json:"tick_size"`
}

type FutureContract struct {
	Exchange        string  `json:"exchange"`
	TradingSymbol   string  `json:"tradingsymbol"`
	Underlying      string  `json:"underlying"`
	Expiry          string  `json:"expiry"`
	InstrumentType  string  `json:"instrument_type"`
	Product         string  `json:"product"`
	DefaultQuantity int     `json:"default_quantity"`
	LotSize         int     `json:"lot_size"`
	TickSize        float64 `json:"tick_size"`
	LastPrice       float64 `json:"last_price,omitempty"`
}

type FutureContractsResponse struct {
	Exchange   string           `json:"exchange"`
	Underlying string           `json:"underlying,omitempty"`
	Contracts  []FutureContract `json:"contracts"`
}

type CacheStatus struct {
	Exchange      string    `json:"exchange"`
	Cached        bool      `json:"cached"`
	Path          string    `json:"path,omitempty"`
	Count         int       `json:"count,omitempty"`
	SyncedAt      time.Time `json:"synced_at,omitempty"`
	RegistryCount int       `json:"registry_count,omitempty"`
}

type HistoricalUnderlying struct {
	Exchange        string   `json:"exchange"`
	Underlying      string   `json:"underlying"`
	InstrumentTypes []string `json:"instrument_types"`
	Count           int      `json:"count"`
}

type HistoricalInstrument struct {
	InstrumentToken uint32  `json:"instrument_token"`
	Exchange        string  `json:"exchange"`
	TradingSymbol   string  `json:"tradingsymbol"`
	Underlying      string  `json:"underlying,omitempty"`
	Expiry          string  `json:"expiry,omitempty"`
	Strike          float64 `json:"strike,omitempty"`
	InstrumentType  string  `json:"instrument_type"`
	Segment         string  `json:"segment"`
	LotSize         int     `json:"lot_size"`
	TickSize        float64 `json:"tick_size"`
	FirstSeen       string  `json:"first_seen,omitempty"`
	LastSeen        string  `json:"last_seen,omitempty"`
}

type HistoricalInstrumentFilter struct {
	Exchange       string
	Underlying     string
	Expiry         string
	InstrumentType string
	TradingSymbol  string
	Limit          int
}

type HistoricalUnderlyingFilter struct {
	Exchange string
	Query    string
	Limit    int
}

type OptionContractsResponse struct {
	Exchange      string           `json:"exchange"`
	Underlying    string           `json:"underlying"`
	Expiry        string           `json:"expiry"`
	CenterStrike  float64          `json:"center_strike,omitempty"`
	CenterSource  string           `json:"center_source,omitempty"`
	RangePoints   float64          `json:"range_points,omitempty"`
	ContractsEach int              `json:"contracts_each_side,omitempty"`
	Contracts     []OptionContract `json:"contracts"`
}

func NewService(client KiteClient, baseDir string) *Service {
	if strings.TrimSpace(baseDir) == "" {
		baseDir = "data"
	}
	return &Service{client: client, baseDir: baseDir, now: time.Now}
}

func (s *Service) Sync(ctx context.Context, exchange string) (*SyncResult, error) {
	exchange = normalizeExchange(exchange)
	items, err := s.client.Instruments(ctx, exchange)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.baseDir, 0o755); err != nil {
		return nil, err
	}
	path := s.cachePath(exchange, s.now())
	payload, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return nil, err
	}
	registryPath, registryCount, err := s.upsertRegistry(exchange, items, s.now())
	if err != nil {
		return nil, err
	}
	return &SyncResult{
		Exchange:      exchange,
		Count:         len(items),
		Path:          path,
		RegistryPath:  registryPath,
		RegistryCount: registryCount,
		SyncedAt:      s.now().UTC(),
	}, nil
}

func (s *Service) CacheStatuses(exchanges []string) ([]CacheStatus, error) {
	if len(exchanges) == 0 {
		exchanges = []string{"NFO", "BFO", "MCX", "NSE", "BSE"}
	}
	registry, registryErr := s.loadRegistry()
	out := make([]CacheStatus, 0, len(exchanges))
	for _, exchange := range exchanges {
		exchange = normalizeExchange(exchange)
		status := CacheStatus{Exchange: exchange}
		path, err := s.latestCachePath(exchange)
		if err == nil {
			status.Cached = true
			status.Path = path
			if info, statErr := os.Stat(path); statErr == nil {
				status.SyncedAt = info.ModTime().UTC()
			}
			if items, loadErr := s.load(exchange); loadErr == nil {
				status.Count = len(items)
			}
		} else if !errors.Is(err, ErrInstrumentCacheMissing) {
			return nil, err
		}
		if registryErr == nil {
			for _, item := range registry {
				if strings.EqualFold(item.Exchange, exchange) {
					status.RegistryCount++
				}
			}
		} else if !os.IsNotExist(registryErr) {
			return nil, registryErr
		}
		out = append(out, status)
	}
	return out, nil
}

func (s *Service) HistoricalUnderlyings(filter HistoricalUnderlyingFilter) ([]HistoricalUnderlying, error) {
	filter.Exchange = normalizeExchange(filter.Exchange)
	filter.Query = strings.ToUpper(strings.TrimSpace(filter.Query))
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	items, err := s.registryOrLatest(filter.Exchange)
	if err != nil {
		return nil, err
	}
	type aggregate struct {
		types map[string]bool
		count int
	}
	seen := map[string]*aggregate{}
	for _, item := range items {
		if !strings.EqualFold(item.Exchange, filter.Exchange) {
			continue
		}
		underlying := strings.ToUpper(strings.TrimSpace(item.Underlying))
		if underlying == "" {
			underlying = strings.ToUpper(strings.TrimSpace(item.TradingSymbol))
		}
		if underlying == "" {
			continue
		}
		if filter.Query != "" && !strings.Contains(underlying, filter.Query) {
			continue
		}
		if seen[underlying] == nil {
			seen[underlying] = &aggregate{types: map[string]bool{}}
		}
		seen[underlying].types[item.InstrumentType] = true
		seen[underlying].count++
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if filter.Query != "" {
			leftPrefix := strings.HasPrefix(keys[i], filter.Query)
			rightPrefix := strings.HasPrefix(keys[j], filter.Query)
			if leftPrefix != rightPrefix {
				return leftPrefix
			}
		}
		if seen[keys[i]].count != seen[keys[j]].count {
			return seen[keys[i]].count > seen[keys[j]].count
		}
		return keys[i] < keys[j]
	})
	if len(keys) > filter.Limit {
		keys = keys[:filter.Limit]
	}
	out := make([]HistoricalUnderlying, 0, len(keys))
	for _, key := range keys {
		out = append(out, HistoricalUnderlying{
			Exchange:        filter.Exchange,
			Underlying:      key,
			InstrumentTypes: sortedKeys(seen[key].types),
			Count:           seen[key].count,
		})
	}
	return out, nil
}

func (s *Service) Underlyings(exchange string) ([]string, error) {
	items, err := s.load(normalizeExchange(exchange))
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, item := range items {
		if !isOption(item) {
			continue
		}
		underlying := underlyingName(item)
		if underlying != "" {
			seen[underlying] = true
		}
	}
	return sortedKeys(seen), nil
}

func (s *Service) FutureUnderlyings(exchange string) ([]string, error) {
	items, err := s.load(normalizeExchange(exchange))
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, item := range items {
		if !isFuture(item) {
			continue
		}
		underlying := futureUnderlyingName(item)
		if underlying != "" {
			seen[underlying] = true
		}
	}
	return sortedKeys(seen), nil
}

func (s *Service) Expiries(exchange, underlying string) ([]string, error) {
	items, err := s.load(normalizeExchange(exchange))
	if err != nil {
		return nil, err
	}
	underlying = strings.ToUpper(strings.TrimSpace(underlying))
	seen := map[string]bool{}
	for _, item := range items {
		if !isOption(item) || !matchesUnderlying(item, underlying) || item.Expiry == "" {
			continue
		}
		seen[item.Expiry] = true
	}
	expiries := sortedKeys(seen)
	sort.Slice(expiries, func(i, j int) bool {
		left, leftOK := parseDate(expiries[i])
		right, rightOK := parseDate(expiries[j])
		if leftOK && rightOK {
			return left.Before(right)
		}
		return expiries[i] < expiries[j]
	})
	return expiries, nil
}

func (s *Service) Futures(exchange, underlying, product string) (*FutureContractsResponse, error) {
	exchange = normalizeExchange(exchange)
	underlying = strings.ToUpper(strings.TrimSpace(underlying))
	if product == "" {
		product = "MIS"
	}
	items, err := s.load(exchange)
	if err != nil {
		return nil, err
	}
	contracts := make([]FutureContract, 0)
	for _, item := range items {
		if !isFuture(item) {
			continue
		}
		if underlying != "" && !matchesFutureUnderlying(item, underlying) {
			continue
		}
		contracts = append(contracts, toFutureContract(item, product))
	}
	sort.Slice(contracts, func(i, j int) bool {
		left, leftOK := parseDate(contracts[i].Expiry)
		right, rightOK := parseDate(contracts[j].Expiry)
		if leftOK && rightOK && !left.Equal(right) {
			return left.Before(right)
		}
		if contracts[i].Underlying == contracts[j].Underlying {
			return contracts[i].TradingSymbol < contracts[j].TradingSymbol
		}
		return contracts[i].Underlying < contracts[j].Underlying
	})
	return &FutureContractsResponse{
		Exchange:   exchange,
		Underlying: underlying,
		Contracts:  contracts,
	}, nil
}

func (s *Service) HistoricalInstruments(filter HistoricalInstrumentFilter) ([]HistoricalInstrument, error) {
	filter.Exchange = normalizeExchange(filter.Exchange)
	filter.Underlying = strings.ToUpper(strings.TrimSpace(filter.Underlying))
	filter.Expiry = strings.TrimSpace(filter.Expiry)
	filter.InstrumentType = strings.ToUpper(strings.TrimSpace(filter.InstrumentType))
	filter.TradingSymbol = strings.ToUpper(strings.TrimSpace(filter.TradingSymbol))
	if filter.Limit <= 0 || filter.Limit > 500 {
		filter.Limit = 100
	}
	items, err := s.registryMergedWithLatest(filter.Exchange)
	if err != nil {
		return nil, err
	}
	return filterHistoricalInstruments(items, filter), nil
}

func (s *Service) ValidateHistoricalInstrument(exchange, tradingSymbol string, instrumentToken int64) error {
	exchange = normalizeExchange(exchange)
	tradingSymbol = strings.ToUpper(strings.TrimSpace(tradingSymbol))
	if tradingSymbol == "" {
		return fmt.Errorf("tradingsymbol is required")
	}
	if instrumentToken <= 0 {
		return fmt.Errorf("instrument_token is required")
	}
	items, err := s.registryOrLatest(exchange)
	if err != nil {
		return err
	}
	var symbolMatch *HistoricalInstrument
	var tokenMatch *HistoricalInstrument
	for i := range items {
		item := &items[i]
		if !strings.EqualFold(item.Exchange, exchange) {
			continue
		}
		if item.TradingSymbol == tradingSymbol && int64(item.InstrumentToken) == instrumentToken {
			return nil
		}
		if item.TradingSymbol == tradingSymbol && symbolMatch == nil {
			symbolMatch = item
		}
		if int64(item.InstrumentToken) == instrumentToken && tokenMatch == nil {
			tokenMatch = item
		}
	}
	if symbolMatch != nil {
		return fmt.Errorf("instrument_token %d does not match %s:%s; expected token %d from instrument registry", instrumentToken, exchange, tradingSymbol, symbolMatch.InstrumentToken)
	}
	if tokenMatch != nil {
		return fmt.Errorf("instrument_token %d belongs to %s:%s, not %s:%s", instrumentToken, tokenMatch.Exchange, tokenMatch.TradingSymbol, exchange, tradingSymbol)
	}
	return fmt.Errorf("instrument %s:%s token %d was not found in instrument registry; run POST /instruments/sync?exchange=%s and select the instrument from the lookup", exchange, tradingSymbol, instrumentToken, exchange)
}

func (s *Service) registryOrLatest(exchange string) ([]HistoricalInstrument, error) {
	return s.registryMergedWithLatest(exchange)
}

func (s *Service) registryMergedWithLatest(exchange string) ([]HistoricalInstrument, error) {
	exchange = normalizeExchange(exchange)
	byKey := map[string]HistoricalInstrument{}
	registry, err := s.loadRegistry()
	if err == nil {
		for _, item := range registry {
			byKey[historicalInstrumentKey(item)] = item
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	cacheItems, cacheErr := s.load(exchange)
	if cacheErr != nil {
		if len(byKey) == 0 {
			return nil, cacheErr
		}
	} else {
		for _, item := range historicalInstrumentsFromKite(cacheItems, s.now()) {
			byKey[historicalInstrumentKey(item)] = item
		}
	}
	out := make([]HistoricalInstrument, 0, len(byKey))
	for _, item := range byKey {
		out = append(out, item)
	}
	sortHistoricalInstruments(out)
	return out, nil
}

func filterHistoricalInstruments(items []HistoricalInstrument, filter HistoricalInstrumentFilter) []HistoricalInstrument {
	out := make([]HistoricalInstrument, 0)
	for _, item := range items {
		if filter.TradingSymbol != "" && !strings.Contains(item.TradingSymbol, filter.TradingSymbol) {
			continue
		}
		if filter.InstrumentType != "" && item.InstrumentType != filter.InstrumentType {
			continue
		}
		if filter.Expiry != "" && item.Expiry != filter.Expiry {
			continue
		}
		if filter.Exchange != "" && !strings.EqualFold(item.Exchange, filter.Exchange) {
			continue
		}
		if filter.Underlying != "" && !strings.EqualFold(item.Underlying, filter.Underlying) && !strings.HasPrefix(item.TradingSymbol, filter.Underlying) {
			continue
		}
		out = append(out, item)
	}
	sortHistoricalInstruments(out)
	if len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out
}

type OptionFilter struct {
	Exchange          string
	Underlying        string
	Expiry            string
	Types             []string
	RangePoints       float64
	ContractsEachSide int
	CenterStrike      float64
	CenterSource      string
	Product           string
}

func (s *Service) Options(ctx context.Context, filter OptionFilter) (*OptionContractsResponse, error) {
	filter.Exchange = normalizeExchange(filter.Exchange)
	filter.Underlying = strings.ToUpper(strings.TrimSpace(filter.Underlying))
	filter.Expiry = strings.TrimSpace(filter.Expiry)
	if filter.Underlying == "" {
		return nil, fmt.Errorf("underlying is required")
	}
	if filter.Expiry == "" {
		expiries, err := s.Expiries(filter.Exchange, filter.Underlying)
		if err != nil {
			return nil, err
		}
		if len(expiries) == 0 {
			return nil, fmt.Errorf("no expiries found for %s %s; sync instruments first", filter.Exchange, filter.Underlying)
		}
		filter.Expiry = expiries[0]
	}
	if filter.RangePoints <= 0 {
		filter.RangePoints = 1000
	}
	if filter.ContractsEachSide <= 0 {
		filter.ContractsEachSide = 10
	}
	if filter.Product == "" {
		filter.Product = "MIS"
	}
	allowedTypes := normalizeTypes(filter.Types)

	items, err := s.load(filter.Exchange)
	if err != nil {
		return nil, err
	}
	contracts := make([]OptionContract, 0)
	for _, item := range items {
		if !isOption(item) || !matchesUnderlying(item, filter.Underlying) || item.Expiry != filter.Expiry || !allowedTypes[item.InstrumentType] {
			continue
		}
		contracts = append(contracts, toOptionContract(item, filter.Product))
	}
	sort.Slice(contracts, func(i, j int) bool {
		if contracts[i].Strike == contracts[j].Strike {
			return contracts[i].InstrumentType < contracts[j].InstrumentType
		}
		return contracts[i].Strike < contracts[j].Strike
	})

	center, source := filter.CenterStrike, "manual"
	if center <= 0 {
		center, source = s.resolveCenterStrike(ctx, filter.Exchange, filter.Underlying, contracts)
	}
	filtered := filterContracts(contracts, center, filter.RangePoints, filter.ContractsEachSide)
	return &OptionContractsResponse{
		Exchange:      filter.Exchange,
		Underlying:    filter.Underlying,
		Expiry:        filter.Expiry,
		CenterStrike:  center,
		CenterSource:  source,
		RangePoints:   filter.RangePoints,
		ContractsEach: filter.ContractsEachSide,
		Contracts:     filtered,
	}, nil
}

func (s *Service) Allows(exchange, tradingSymbol string) bool {
	items, err := s.load(normalizeExchange(exchange))
	if err != nil {
		return false
	}
	tradingSymbol = strings.ToUpper(strings.TrimSpace(tradingSymbol))
	for _, item := range items {
		if strings.EqualFold(item.Exchange, exchange) && item.TradingSymbol == tradingSymbol {
			return true
		}
	}
	return false
}

func (s *Service) load(exchange string) ([]kite.Instrument, error) {
	path, err := s.latestCachePath(exchange)
	if err != nil {
		return nil, err
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var items []kite.Instrument
	if err := json.Unmarshal(payload, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) cachePath(exchange string, now time.Time) string {
	return filepath.Join(s.baseDir, "instruments_"+exchange+"_"+now.Format("02_01_2006")+".json")
}

func (s *Service) latestCachePath(exchange string) (string, error) {
	pattern := filepath.Join(s.baseDir, "instruments_"+exchange+"_*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("%w: no cached instruments for %s; run POST /instruments/sync?exchange=%s", ErrInstrumentCacheMissing, exchange, exchange)
	}
	latest := matches[0]
	latestTime := time.Time{}
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			continue
		}
		if info.ModTime().After(latestTime) {
			latest = match
			latestTime = info.ModTime()
		}
	}
	return latest, nil
}

func (s *Service) LotSize(exchange, tradingSymbol string) (int, bool) {
	exchange = normalizeExchange(exchange)
	items, err := s.load(exchange)
	if err != nil {
		return 0, false
	}
	tradingSymbol = strings.ToUpper(strings.TrimSpace(tradingSymbol))
	for _, item := range items {
		if strings.EqualFold(item.Exchange, exchange) && item.TradingSymbol == tradingSymbol && item.LotSize > 0 {
			return item.LotSize, true
		}
	}
	return 0, false
}

func normalizeExchange(exchange string) string {
	exchange = strings.ToUpper(strings.TrimSpace(exchange))
	if exchange == "" {
		return "NFO"
	}
	return exchange
}

func isOption(item kite.Instrument) bool {
	return item.InstrumentType == "CE" || item.InstrumentType == "PE"
}

func isFuture(item kite.Instrument) bool {
	return item.InstrumentType == "FUT" || strings.HasSuffix(strings.ToUpper(item.Segment), "-FUT")
}

func underlyingName(item kite.Instrument) string {
	if item.Name != "" {
		return strings.ToUpper(item.Name)
	}
	for _, suffix := range []string{"CE", "PE"} {
		if strings.HasSuffix(item.TradingSymbol, suffix) {
			return strings.TrimRightFunc(strings.TrimSuffix(item.TradingSymbol, suffix), func(r rune) bool {
				return (r >= '0' && r <= '9') || r == '.'
			})
		}
	}
	return ""
}

func futureUnderlyingName(item kite.Instrument) string {
	if item.Name != "" {
		return strings.ToUpper(strings.TrimSpace(item.Name))
	}
	symbol := strings.ToUpper(strings.TrimSpace(item.TradingSymbol))
	if strings.HasSuffix(symbol, "FUT") {
		symbol = strings.TrimSuffix(symbol, "FUT")
	}
	return strings.TrimRightFunc(symbol, func(r rune) bool {
		return (r >= '0' && r <= '9') || r == '.'
	})
}

func historicalUnderlyingName(item kite.Instrument) string {
	if isFuture(item) {
		return futureUnderlyingName(item)
	}
	return underlyingName(item)
}

func matchesUnderlying(item kite.Instrument, underlying string) bool {
	if underlying == "" {
		return true
	}
	if strings.EqualFold(underlyingName(item), underlying) {
		return true
	}
	return strings.HasPrefix(item.TradingSymbol, underlying)
}

func matchesFutureUnderlying(item kite.Instrument, underlying string) bool {
	if underlying == "" {
		return true
	}
	if strings.EqualFold(futureUnderlyingName(item), underlying) {
		return true
	}
	return strings.HasPrefix(strings.ToUpper(item.TradingSymbol), underlying)
}

func normalizeTypes(types []string) map[string]bool {
	out := map[string]bool{"CE": true, "PE": true}
	if len(types) == 0 {
		return out
	}
	out = map[string]bool{}
	for _, typ := range types {
		typ = strings.ToUpper(strings.TrimSpace(typ))
		if typ == "CE" || typ == "PE" {
			out[typ] = true
		}
	}
	if len(out) == 0 {
		out["CE"] = true
		out["PE"] = true
	}
	return out
}

func toFutureContract(item kite.Instrument, product string) FutureContract {
	lotSize := item.LotSize
	if lotSize <= 0 {
		lotSize = 1
	}
	return FutureContract{
		Exchange:        item.Exchange,
		TradingSymbol:   item.TradingSymbol,
		Underlying:      futureUnderlyingName(item),
		Expiry:          item.Expiry,
		InstrumentType:  item.InstrumentType,
		Product:         strings.ToUpper(product),
		DefaultQuantity: lotSize,
		LotSize:         lotSize,
		TickSize:        item.TickSize,
		LastPrice:       item.LastPrice,
	}
}

func toOptionContract(item kite.Instrument, product string) OptionContract {
	lotSize := item.LotSize
	if lotSize <= 0 {
		lotSize = 1
	}
	return OptionContract{
		Exchange:        item.Exchange,
		TradingSymbol:   item.TradingSymbol,
		Underlying:      underlyingName(item),
		Expiry:          item.Expiry,
		Strike:          item.Strike,
		InstrumentType:  item.InstrumentType,
		Product:         strings.ToUpper(product),
		DefaultQuantity: lotSize,
		LotSize:         lotSize,
		TickSize:        item.TickSize,
	}
}

func (s *Service) resolveCenterStrike(ctx context.Context, exchange, underlying string, contracts []OptionContract) (float64, string) {
	if len(contracts) == 0 {
		return 0, ""
	}
	for _, candidate := range ltpCandidates(exchange, underlying) {
		ltp, err := s.client.LTP(ctx, candidate.exchange, candidate.symbol)
		if err == nil && ltp > 0 {
			return nearestStrike(contracts, ltp), "ltp"
		}
	}
	return medianStrike(contracts), "median"
}

type ltpCandidate struct {
	exchange string
	symbol   string
}

func ltpCandidates(exchange, underlying string) []ltpCandidate {
	underlying = strings.ToUpper(strings.TrimSpace(underlying))
	switch underlying {
	case "NIFTY":
		return []ltpCandidate{{exchange: "NSE", symbol: "NIFTY 50"}}
	case "BANKNIFTY":
		return []ltpCandidate{{exchange: "NSE", symbol: "NIFTY BANK"}}
	case "FINNIFTY":
		return []ltpCandidate{{exchange: "NSE", symbol: "NIFTY FIN SERVICE"}}
	case "MIDCPNIFTY":
		return []ltpCandidate{{exchange: "NSE", symbol: "NIFTY MID SELECT"}}
	case "SENSEX":
		return []ltpCandidate{{exchange: "BSE", symbol: "SENSEX"}}
	default:
		if strings.EqualFold(exchange, "BFO") {
			return []ltpCandidate{{exchange: "BSE", symbol: underlying}, {exchange: "NSE", symbol: underlying}}
		}
		return []ltpCandidate{{exchange: "NSE", symbol: underlying}, {exchange: "BSE", symbol: underlying}}
	}
}

func filterContracts(contracts []OptionContract, center, rangePoints float64, contractsEachSide int) []OptionContract {
	if len(contracts) == 0 || center <= 0 {
		return contracts
	}
	byRange := make([]OptionContract, 0, len(contracts))
	for _, contract := range contracts {
		if math.Abs(contract.Strike-center) <= rangePoints {
			byRange = append(byRange, contract)
		}
	}
	if len(byRange) == 0 {
		byRange = contracts
	}
	strikes := uniqueStrikes(byRange)
	sort.Slice(strikes, func(i, j int) bool {
		return math.Abs(strikes[i]-center) < math.Abs(strikes[j]-center)
	})
	limit := contractsEachSide*2 + 1
	if limit > len(strikes) {
		limit = len(strikes)
	}
	allowed := map[float64]bool{}
	for _, strike := range strikes[:limit] {
		allowed[strike] = true
	}
	out := make([]OptionContract, 0)
	for _, contract := range byRange {
		if allowed[contract.Strike] {
			out = append(out, contract)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Strike == out[j].Strike {
			return out[i].InstrumentType < out[j].InstrumentType
		}
		return out[i].Strike < out[j].Strike
	})
	return out
}

func nearestStrike(contracts []OptionContract, ltp float64) float64 {
	best := contracts[0].Strike
	bestDistance := math.Abs(best - ltp)
	for _, contract := range contracts[1:] {
		distance := math.Abs(contract.Strike - ltp)
		if distance < bestDistance {
			best = contract.Strike
			bestDistance = distance
		}
	}
	return best
}

func medianStrike(contracts []OptionContract) float64 {
	strikes := uniqueStrikes(contracts)
	if len(strikes) == 0 {
		return 0
	}
	sort.Float64s(strikes)
	return strikes[len(strikes)/2]
}

func uniqueStrikes(contracts []OptionContract) []float64 {
	seen := map[string]float64{}
	for _, contract := range contracts {
		key := strconv.FormatFloat(contract.Strike, 'f', 2, 64)
		seen[key] = contract.Strike
	}
	out := make([]float64, 0, len(seen))
	for _, strike := range seen {
		out = append(out, strike)
	}
	return out
}

func sortedKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func parseDate(raw string) (time.Time, bool) {
	parsed, err := time.Parse("2006-01-02", raw)
	return parsed, err == nil
}
