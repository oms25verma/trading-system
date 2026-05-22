package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr           string
	KiteAPIKey     string
	KiteAPISecret  string
	AccessToken    string
	TradeStorePath string
	PollInterval   time.Duration
}

func Load() Config {
	pollSeconds := int64(5)
	if raw := os.Getenv("POLL_SECONDS"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			pollSeconds = parsed
		}
	}

	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	tradeStorePath := os.Getenv("TRADE_STORE_PATH")
	if tradeStorePath == "" {
		tradeStorePath = "data/trades.json"
	}

	return Config{
		Addr:           addr,
		KiteAPIKey:     os.Getenv("KITE_API_KEY"),
		KiteAPISecret:  os.Getenv("KITE_API_SECRET"),
		AccessToken:    os.Getenv("KITE_ACCESS_TOKEN"),
		TradeStorePath: tradeStorePath,
		PollInterval:   time.Duration(pollSeconds) * time.Second,
	}
}
