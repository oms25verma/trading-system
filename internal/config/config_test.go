package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadSyncPollIntervalDefaultsDisabled(t *testing.T) {
	t.Setenv("SYNC_POLL_SECONDS", "")

	cfg := Load()

	if cfg.SyncPollInterval != 0 {
		t.Fatalf("expected sync poll disabled by default, got %s", cfg.SyncPollInterval)
	}
}

func TestLoadSyncPollIntervalFromEnv(t *testing.T) {
	t.Setenv("SYNC_POLL_SECONDS", "30")

	cfg := Load()

	if cfg.SyncPollInterval != 30*time.Second {
		t.Fatalf("expected 30s sync poll interval, got %s", cfg.SyncPollInterval)
	}
}

func TestLoadSymbolWatchlistFromEnv(t *testing.T) {
	t.Setenv("DEFAULT_PRODUCT", "MIS")
	t.Setenv("SYMBOL_WATCHLIST", "nse:infy, mcx:silverm26junfut:NRML")

	cfg := Load()

	if len(cfg.SymbolWatchlist) != 2 {
		t.Fatalf("expected two watchlist items, got %+v", cfg.SymbolWatchlist)
	}
	if cfg.SymbolWatchlist[0].Exchange != "NSE" || cfg.SymbolWatchlist[0].TradingSymbol != "INFY" || cfg.SymbolWatchlist[0].Product != "MIS" {
		t.Fatalf("unexpected first symbol: %+v", cfg.SymbolWatchlist[0])
	}
	if cfg.SymbolWatchlist[1].Exchange != "MCX" || cfg.SymbolWatchlist[1].TradingSymbol != "SILVERM26JUNFUT" || cfg.SymbolWatchlist[1].Product != "NRML" {
		t.Fatalf("unexpected second symbol: %+v", cfg.SymbolWatchlist[1])
	}
}

func TestLoadSymbolWatchlistFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "symbols.json")
	payload := []byte(`{
		"symbols": [
			{
				"exchange": "mcx",
				"symbol": "silverm26junfut",
				"products": ["MIS", "NRML"],
				"name": "Silver Mini Jun Fut",
				"default_quantity": 1,
				"lot_size": 1,
				"tick_size": 1
			},
			{
				"exchange": "nse",
				"symbol": "reliance",
				"enabled": false
			}
		]
	}`)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEFAULT_PRODUCT", "MIS")
	t.Setenv("SYMBOL_WATCHLIST", "NSE:INFY:MIS")
	t.Setenv("SYMBOL_WATCHLIST_FILE", path)

	cfg := Load()

	if err := cfg.Validate("paper"); err != nil {
		t.Fatal(err)
	}
	if len(cfg.SymbolWatchlist) != 2 {
		t.Fatalf("expected two enabled product rows, got %+v", cfg.SymbolWatchlist)
	}
	first := cfg.SymbolWatchlist[0]
	if first.Exchange != "MCX" || first.TradingSymbol != "SILVERM26JUNFUT" || first.Product != "MIS" || first.Name != "Silver Mini Jun Fut" {
		t.Fatalf("unexpected first symbol: %+v", first)
	}
	second := cfg.SymbolWatchlist[1]
	if second.Product != "NRML" || second.LotSize != 1 || second.TickSize != 1 {
		t.Fatalf("unexpected second symbol: %+v", second)
	}
}

func TestLoadSymbolWatchlistFileErrorFailsValidation(t *testing.T) {
	t.Setenv("SYMBOL_WATCHLIST_FILE", filepath.Join(t.TempDir(), "missing.json"))

	cfg := Load()

	if err := cfg.Validate("paper"); err == nil {
		t.Fatal("expected missing symbol watchlist file to fail validation")
	}
}
