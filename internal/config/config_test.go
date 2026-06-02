package config

import (
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
