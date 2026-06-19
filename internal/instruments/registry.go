package instruments

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"trading-system/internal/kite"
)

const instrumentRegistryFile = "instrument_registry.json"

func (s *Service) upsertRegistry(exchange string, items []kite.Instrument, seenAt time.Time) (string, int, error) {
	exchange = normalizeExchange(exchange)
	seenAt = seenAt.UTC()
	registry, err := s.loadRegistry()
	if err != nil && !os.IsNotExist(err) {
		return "", 0, err
	}
	byKey := make(map[string]HistoricalInstrument, len(registry)+len(items))
	for _, item := range registry {
		byKey[historicalInstrumentKey(item)] = item
	}
	for _, item := range historicalInstrumentsFromKite(items, seenAt) {
		if !strings.EqualFold(item.Exchange, exchange) {
			continue
		}
		key := historicalInstrumentKey(item)
		existing, ok := byKey[key]
		if ok {
			if existing.FirstSeen != "" {
				item.FirstSeen = existing.FirstSeen
			}
			item.LastSeen = seenAt.Format("2006-01-02")
		}
		byKey[key] = item
	}
	out := make([]HistoricalInstrument, 0, len(byKey))
	for _, item := range byKey {
		out = append(out, item)
	}
	sortHistoricalInstruments(out)
	path := s.registryPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", 0, err
	}
	payload, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", 0, err
	}
	payload = append(payload, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return "", 0, err
	}
	return path, len(out), os.Rename(tmp, path)
}

func (s *Service) loadRegistry() ([]HistoricalInstrument, error) {
	payload, err := os.ReadFile(s.registryPath())
	if err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		return nil, nil
	}
	var items []HistoricalInstrument
	if err := json.Unmarshal(payload, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) registryPath() string {
	return filepath.Join(s.baseDir, instrumentRegistryFile)
}

func historicalInstrumentsFromKite(items []kite.Instrument, seenAt time.Time) []HistoricalInstrument {
	seenDay := seenAt.UTC().Format("2006-01-02")
	out := make([]HistoricalInstrument, 0, len(items))
	for _, item := range items {
		out = append(out, HistoricalInstrument{
			InstrumentToken: item.InstrumentToken,
			Exchange:        item.Exchange,
			TradingSymbol:   strings.ToUpper(strings.TrimSpace(item.TradingSymbol)),
			Underlying:      historicalUnderlyingName(item),
			Expiry:          item.Expiry,
			Strike:          item.Strike,
			InstrumentType:  strings.ToUpper(strings.TrimSpace(item.InstrumentType)),
			Segment:         item.Segment,
			LotSize:         item.LotSize,
			TickSize:        item.TickSize,
			FirstSeen:       seenDay,
			LastSeen:        seenDay,
		})
	}
	return out
}

func historicalInstrumentKey(item HistoricalInstrument) string {
	parts := []string{
		strings.ToUpper(strings.TrimSpace(item.Exchange)),
		strings.ToUpper(strings.TrimSpace(item.TradingSymbol)),
		item.Expiry,
		strconv.FormatFloat(item.Strike, 'f', 2, 64),
		strings.ToUpper(strings.TrimSpace(item.InstrumentType)),
	}
	return strings.Join(parts, "|")
}

func sortHistoricalInstruments(items []HistoricalInstrument) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Exchange != items[j].Exchange {
			return items[i].Exchange < items[j].Exchange
		}
		if items[i].Expiry != items[j].Expiry {
			left, leftOK := parseDate(items[i].Expiry)
			right, rightOK := parseDate(items[j].Expiry)
			if leftOK && rightOK {
				return left.Before(right)
			}
			return items[i].Expiry < items[j].Expiry
		}
		if items[i].Underlying != items[j].Underlying {
			return items[i].Underlying < items[j].Underlying
		}
		if items[i].Strike != items[j].Strike {
			return items[i].Strike < items[j].Strike
		}
		if items[i].InstrumentType != items[j].InstrumentType {
			return items[i].InstrumentType < items[j].InstrumentType
		}
		return items[i].TradingSymbol < items[j].TradingSymbol
	})
}
