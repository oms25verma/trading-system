package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr                    string
	KiteAPIKey              string
	KiteAPISecret           string
	AccessToken             string
	TradeStorePath          string
	SymbolWatchlist         []SymbolWatchItem
	SymbolWatchlistFile     string
	symbolWatchlistErr      error
	PollInterval            time.Duration
	SyncPollInterval        time.Duration
	DefaultProduct          string
	DefaultQuantity         int
	DefaultMarketProtection *int
	DefaultStopLossPoints   float64
	DefaultTargetPoints     float64
	DefaultSLLimitOffset    float64
	LogLevel                string
}

type SymbolWatchItem struct {
	Exchange        string  `json:"exchange"`
	TradingSymbol   string  `json:"tradingsymbol"`
	Product         string  `json:"product"`
	Name            string  `json:"name,omitempty"`
	DefaultQuantity int     `json:"default_quantity,omitempty"`
	LotSize         int     `json:"lot_size,omitempty"`
	TickSize        float64 `json:"tick_size,omitempty"`
}

func Load() Config {
	pollSeconds := int64(5)
	if raw := os.Getenv("POLL_SECONDS"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			pollSeconds = parsed
		}
	}
	syncPollSeconds := int64(0)
	if raw := os.Getenv("SYNC_POLL_SECONDS"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			syncPollSeconds = parsed
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
	symbolWatchlistFile := os.Getenv("SYMBOL_WATCHLIST_FILE")
	symbolWatchlist, symbolWatchlistErr := loadSymbolWatchlist(symbolWatchlistFile, os.Getenv("SYMBOL_WATCHLIST"), defaultProduct)

	return Config{
		Addr:                    addr,
		KiteAPIKey:              os.Getenv("KITE_API_KEY"),
		KiteAPISecret:           os.Getenv("KITE_API_SECRET"),
		AccessToken:             os.Getenv("KITE_ACCESS_TOKEN"),
		TradeStorePath:          tradeStorePath,
		SymbolWatchlist:         symbolWatchlist,
		SymbolWatchlistFile:     symbolWatchlistFile,
		symbolWatchlistErr:      symbolWatchlistErr,
		PollInterval:            time.Duration(pollSeconds) * time.Second,
		SyncPollInterval:        time.Duration(syncPollSeconds) * time.Second,
		DefaultProduct:          defaultProduct,
		DefaultQuantity:         intEnv("DEFAULT_QUANTITY", 1),
		DefaultMarketProtection: optionalIntEnv("DEFAULT_MARKET_PROTECTION"),
		DefaultStopLossPoints:   floatEnv("DEFAULT_STOP_LOSS_POINTS", 0),
		DefaultTargetPoints:     floatEnv("DEFAULT_TARGET_POINTS", 0),
		DefaultSLLimitOffset:    floatEnv("DEFAULT_SL_LIMIT_OFFSET", 0),
		LogLevel:                stringEnv("LOG_LEVEL", "info"),
	}
}

func loadSymbolWatchlist(filePath, raw, defaultProduct string) ([]SymbolWatchItem, error) {
	if strings.TrimSpace(filePath) != "" {
		items, err := parseSymbolWatchlistFile(filePath, defaultProduct)
		if err != nil {
			return nil, err
		}
		return items, nil
	}
	return parseSymbolWatchlist(raw, defaultProduct), nil
}

type symbolWatchlistFile struct {
	Symbols []rawSymbolWatchItem `json:"symbols"`
}

type rawSymbolWatchItem struct {
	Exchange        string   `json:"exchange"`
	TradingSymbol   string   `json:"tradingsymbol"`
	Symbol          string   `json:"symbol"`
	Product         string   `json:"product"`
	Name            string   `json:"name"`
	DefaultQuantity int      `json:"default_quantity"`
	LotSize         int      `json:"lot_size"`
	TickSize        float64  `json:"tick_size"`
	Enabled         *bool    `json:"enabled"`
	Products        []string `json:"products"`
}

func parseSymbolWatchlistFile(path, defaultProduct string) ([]SymbolWatchItem, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read SYMBOL_WATCHLIST_FILE: %w", err)
	}
	var rawItems []rawSymbolWatchItem
	if err := json.Unmarshal(payload, &rawItems); err != nil {
		var file symbolWatchlistFile
		if nestedErr := json.Unmarshal(payload, &file); nestedErr != nil {
			return nil, fmt.Errorf("parse SYMBOL_WATCHLIST_FILE: %w", err)
		}
		rawItems = file.Symbols
	}
	return normalizeSymbolWatchlist(rawItems, defaultProduct), nil
}

func parseSymbolWatchlist(raw, defaultProduct string) []SymbolWatchItem {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	rawItems := make([]rawSymbolWatchItem, 0)
	for _, token := range strings.Split(raw, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		parts := strings.Split(token, ":")
		if len(parts) < 2 {
			continue
		}
		product := defaultProduct
		if len(parts) >= 3 && strings.TrimSpace(parts[2]) != "" {
			product = strings.TrimSpace(parts[2])
		}
		rawItems = append(rawItems, rawSymbolWatchItem{
			Exchange:      parts[0],
			TradingSymbol: parts[1],
			Product:       product,
		})
	}
	return normalizeSymbolWatchlist(rawItems, defaultProduct)
}

func normalizeSymbolWatchlist(rawItems []rawSymbolWatchItem, defaultProduct string) []SymbolWatchItem {
	items := make([]SymbolWatchItem, 0, len(rawItems))
	for _, raw := range rawItems {
		if raw.Enabled != nil && !*raw.Enabled {
			continue
		}
		symbol := strings.TrimSpace(raw.TradingSymbol)
		if symbol == "" {
			symbol = strings.TrimSpace(raw.Symbol)
		}
		if strings.TrimSpace(raw.Exchange) == "" || symbol == "" {
			continue
		}
		products := raw.Products
		if len(products) == 0 {
			product := strings.TrimSpace(raw.Product)
			if product == "" {
				product = defaultProduct
			}
			products = []string{product}
		}
		for _, product := range products {
			product = strings.TrimSpace(product)
			if product == "" {
				product = defaultProduct
			}
			defaultQuantity := raw.DefaultQuantity
			if defaultQuantity < 0 {
				defaultQuantity = 0
			}
			lotSize := raw.LotSize
			if lotSize < 0 {
				lotSize = 0
			}
			tickSize := raw.TickSize
			if tickSize < 0 {
				tickSize = 0
			}
			items = append(items, SymbolWatchItem{
				Exchange:        strings.ToUpper(strings.TrimSpace(raw.Exchange)),
				TradingSymbol:   strings.ToUpper(symbol),
				Product:         strings.ToUpper(product),
				Name:            strings.TrimSpace(raw.Name),
				DefaultQuantity: defaultQuantity,
				LotSize:         lotSize,
				TickSize:        tickSize,
			})
		}
	}
	return items
}

func stringEnv(key, fallback string) string {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	return raw
}

func (c Config) Validate(broker string) error {
	if c.Addr == "" {
		return fmt.Errorf("HTTP_ADDR is required")
	}
	if c.PollInterval <= 0 {
		return fmt.Errorf("POLL_SECONDS must be positive")
	}
	if c.SyncPollInterval < 0 {
		return fmt.Errorf("SYNC_POLL_SECONDS cannot be negative")
	}
	if c.TradeStorePath == "" {
		return fmt.Errorf("TRADE_STORE_PATH is required")
	}
	if c.symbolWatchlistErr != nil {
		return c.symbolWatchlistErr
	}
	if c.DefaultProduct == "" {
		return fmt.Errorf("DEFAULT_PRODUCT is required")
	}
	if c.DefaultQuantity <= 0 {
		return fmt.Errorf("DEFAULT_QUANTITY must be positive")
	}
	if c.DefaultStopLossPoints < 0 {
		return fmt.Errorf("DEFAULT_STOP_LOSS_POINTS cannot be negative")
	}
	if c.DefaultTargetPoints < 0 {
		return fmt.Errorf("DEFAULT_TARGET_POINTS cannot be negative")
	}
	if c.DefaultSLLimitOffset < 0 {
		return fmt.Errorf("DEFAULT_SL_LIMIT_OFFSET cannot be negative")
	}
	if strings.EqualFold(broker, "kite") {
		if c.KiteAPIKey == "" {
			return fmt.Errorf("KITE_API_KEY is required when BROKER=kite")
		}
		if c.KiteAPISecret == "" {
			return fmt.Errorf("KITE_API_SECRET is required when BROKER=kite")
		}
		if c.AccessToken == "" {
			return fmt.Errorf("KITE_ACCESS_TOKEN is required when BROKER=kite")
		}
	}
	return nil
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
