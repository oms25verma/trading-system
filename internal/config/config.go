package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr                    string
	KiteAPIKey              string
	KiteAPISecret           string
	AccessToken             string
	TradeStorePath          string
	PollInterval            time.Duration
	DefaultProduct          string
	DefaultQuantity         int
	DefaultMarketProtection *int
	DefaultStopLossPoints   float64
	DefaultTargetPoints     float64
	DefaultSLLimitOffset    float64
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
		tradeStorePath = "data"
	}

	defaultProduct := os.Getenv("DEFAULT_PRODUCT")
	if defaultProduct == "" {
		defaultProduct = "MIS"
	}

	return Config{
		Addr:                    addr,
		KiteAPIKey:              os.Getenv("KITE_API_KEY"),
		KiteAPISecret:           os.Getenv("KITE_API_SECRET"),
		AccessToken:             os.Getenv("KITE_ACCESS_TOKEN"),
		TradeStorePath:          tradeStorePath,
		PollInterval:            time.Duration(pollSeconds) * time.Second,
		DefaultProduct:          defaultProduct,
		DefaultQuantity:         intEnv("DEFAULT_QUANTITY", 1),
		DefaultMarketProtection: optionalIntEnv("DEFAULT_MARKET_PROTECTION"),
		DefaultStopLossPoints:   floatEnv("DEFAULT_STOP_LOSS_POINTS", 0),
		DefaultTargetPoints:     floatEnv("DEFAULT_TARGET_POINTS", 0),
		DefaultSLLimitOffset:    floatEnv("DEFAULT_SL_LIMIT_OFFSET", 0),
	}
}

func intEnv(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func optionalIntEnv(key string) *int {
	raw := os.Getenv(key)
	if raw == "" {
		return nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	return &parsed
}

func floatEnv(key string, fallback float64) float64 {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}
