package trading

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Store interface {
	Load() ([]*ManagedTrade, error)
	Save(trades []*ManagedTrade) error
}

type OrderStore interface {
	LoadOrders() ([]*KiteOrder, error)
	SaveOrders(orders []*KiteOrder) error
}

type PositionStore interface {
	LoadPositions() ([]*KitePosition, error)
	SavePositions(positions []*KitePosition) error
}

type JSONStore struct {
	path string
}

type JSONOrderStore struct {
	path string
}

type JSONPositionStore struct {
	path string
}

func NewJSONStore(path string) *JSONStore {
	return &JSONStore{path: dailyStorePath(path, time.Now())}
}

func NewJSONOrderStore(path string) *JSONOrderStore {
	return &JSONOrderStore{path: dailyOrderStorePath(path, time.Now())}
}

func NewJSONPositionStore(path string) *JSONPositionStore {
	return &JSONPositionStore{path: dailyPositionStorePath(path, time.Now())}
}

func (s *JSONStore) Load() ([]*ManagedTrade, error) {
	payload, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if len(payload) == 0 {
		return nil, nil
	}

	var trades []*ManagedTrade
	if err := json.Unmarshal(payload, &trades); err != nil {
		return nil, err
	}
	return trades, nil
}

func (s *JSONStore) Save(trades []*ManagedTrade) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	sort.Slice(trades, func(i, j int) bool {
		return trades[i].CreatedAt.Before(trades[j].CreatedAt)
	})

	payload, err := json.MarshalIndent(trades, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *JSONOrderStore) LoadOrders() ([]*KiteOrder, error) {
	payload, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if len(payload) == 0 {
		return nil, nil
	}

	var orders []*KiteOrder
	if err := json.Unmarshal(payload, &orders); err != nil {
		return nil, err
	}
	return orders, nil
}

func (s *JSONOrderStore) SaveOrders(orders []*KiteOrder) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	sort.Slice(orders, func(i, j int) bool {
		return orders[i].OrderID < orders[j].OrderID
	})

	payload, err := json.MarshalIndent(orders, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *JSONPositionStore) LoadPositions() ([]*KitePosition, error) {
	payload, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if len(payload) == 0 {
		return nil, nil
	}

	var positions []*KitePosition
	if err := json.Unmarshal(payload, &positions); err != nil {
		return nil, err
	}
	return positions, nil
}

func (s *JSONPositionStore) SavePositions(positions []*KitePosition) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	sort.Slice(positions, func(i, j int) bool {
		return positionKey(positions[i].Exchange, positions[i].TradingSymbol, positions[i].Product) < positionKey(positions[j].Exchange, positions[j].TradingSymbol, positions[j].Product)
	})

	payload, err := json.MarshalIndent(positions, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func dailyStorePath(path string, now time.Time) string {
	if strings.EqualFold(filepath.Ext(path), ".json") {
		return path
	}
	return filepath.Join(path, "trades_"+now.Format("02_01_2006")+".json")
}

func dailyOrderStorePath(path string, now time.Time) string {
	if strings.EqualFold(filepath.Ext(path), ".json") {
		ext := filepath.Ext(path)
		return strings.TrimSuffix(path, ext) + "_orders" + ext
	}
	return filepath.Join(path, "orders_"+now.Format("02_01_2006")+".json")
}

func dailyPositionStorePath(path string, now time.Time) string {
	if strings.EqualFold(filepath.Ext(path), ".json") {
		ext := filepath.Ext(path)
		return strings.TrimSuffix(path, ext) + "_positions" + ext
	}
	return filepath.Join(path, "positions_"+now.Format("02_01_2006")+".json")
}
