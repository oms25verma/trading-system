package trading

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
)

type Store interface {
	Load() ([]*ManagedTrade, error)
	Save(trades []*ManagedTrade) error
}

type JSONStore struct {
	path string
}

func NewJSONStore(path string) *JSONStore {
	return &JSONStore{path: path}
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
