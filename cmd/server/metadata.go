package main

import (
	"os"

	"trading-system/internal/config"
	"trading-system/internal/trading"
)

type metadataResponse struct {
	Runtime      runtimeMetadata     `json:"runtime"`
	Enums        enumMetadata        `json:"enums"`
	Capabilities capabilityMetadata  `json:"capabilities"`
	Endpoints    map[string][]string `json:"endpoints"`
}

type runtimeMetadata struct {
	Broker                  string           `json:"broker"`
	HTTPAddr                string           `json:"http_addr"`
	TradeStorePath          string           `json:"trade_store_path"`
	PollSeconds             int              `json:"poll_seconds"`
	SyncPollSeconds         int              `json:"sync_poll_seconds"`
	LogLevel                string           `json:"log_level"`
	ErrorLogPath            string           `json:"error_log_path"`
	DefaultProduct          string           `json:"default_product"`
	DefaultQuantity         int              `json:"default_quantity"`
	DefaultMarketProtection *int             `json:"default_market_protection,omitempty"`
	DefaultStopLossPoints   float64          `json:"default_stop_loss_points"`
	DefaultTargetPoints     float64          `json:"default_target_points"`
	DefaultSLLimitOffset    float64          `json:"default_sl_limit_offset"`
	EnforceSymbolWatchlist  bool             `json:"enforce_symbol_watchlist"`
	RequireOrderProtection  bool             `json:"require_order_protection"`
	SymbolWatchlist         []symbolMetadata `json:"symbol_watchlist,omitempty"`
	KiteAPIKeyConfigured    bool             `json:"kite_api_key_configured"`
	KiteAccessConfigured    bool             `json:"kite_access_configured"`
	DeferredItems           []string         `json:"deferred_items,omitempty"`
}

type symbolMetadata struct {
	Exchange        string  `json:"exchange"`
	TradingSymbol   string  `json:"tradingsymbol"`
	Product         string  `json:"product"`
	Name            string  `json:"name,omitempty"`
	DefaultQuantity int     `json:"default_quantity,omitempty"`
	LotSize         int     `json:"lot_size,omitempty"`
	TickSize        float64 `json:"tick_size,omitempty"`
}

type enumMetadata struct {
	Sides              []string `json:"sides"`
	Products           []string `json:"products"`
	OrderTypes         []string `json:"order_types"`
	OrderStatuses      []string `json:"order_statuses"`
	TradeStatuses      []string `json:"trade_statuses"`
	CreationSources    []string `json:"creation_sources"`
	ManagementStatuses []string `json:"management_statuses"`
	ExitReasons        []string `json:"exit_reasons"`
	Warnings           []string `json:"warnings"`
	ExternalExitRoles  []string `json:"external_exit_roles"`
	RiskStatuses       []string `json:"risk_statuses"`
}

type capabilityMetadata struct {
	PaperBroker               bool `json:"paper_broker"`
	KiteBroker                bool `json:"kite_broker"`
	KiteAuth                  bool `json:"kite_auth"`
	KiteSync                  bool `json:"kite_sync"`
	OrderEntry                bool `json:"order_entry"`
	OrderbookCancel           bool `json:"orderbook_cancel"`
	TradeLevelProtection      bool `json:"trade_level_protection"`
	GroupLevelSingleTradeOps  bool `json:"group_level_single_trade_ops"`
	ExternalPositionTakeover  bool `json:"external_position_takeover"`
	ExternalExitManualLinking bool `json:"external_exit_manual_linking"`
	PollingOCO                bool `json:"polling_oco"`
	DatabasePersistence       bool `json:"database_persistence"`
	KillSwitch                bool `json:"kill_switch"`
	Analytics                 bool `json:"analytics"`
}

func buildMetadata(cfg config.Config) metadataResponse {
	broker := brokerName()
	return metadataResponse{
		Runtime: runtimeMetadata{
			Broker:                  broker,
			HTTPAddr:                cfg.Addr,
			TradeStorePath:          cfg.TradeStorePath,
			PollSeconds:             int(cfg.PollInterval.Seconds()),
			SyncPollSeconds:         int(cfg.SyncPollInterval.Seconds()),
			LogLevel:                cfg.LogLevel,
			ErrorLogPath:            cfg.ErrorLogPath,
			DefaultProduct:          cfg.DefaultProduct,
			DefaultQuantity:         cfg.DefaultQuantity,
			DefaultMarketProtection: cfg.DefaultMarketProtection,
			DefaultStopLossPoints:   cfg.DefaultStopLossPoints,
			DefaultTargetPoints:     cfg.DefaultTargetPoints,
			DefaultSLLimitOffset:    cfg.DefaultSLLimitOffset,
			EnforceSymbolWatchlist:  cfg.EnforceSymbolWatchlist,
			RequireOrderProtection:  cfg.RequireOrderProtection,
			SymbolWatchlist:         symbolWatchlistMetadata(cfg.SymbolWatchlist),
			KiteAPIKeyConfigured:    cfg.KiteAPIKey != "",
			KiteAccessConfigured:    cfg.AccessToken != "",
			DeferredItems: []string{
				"database_persistence",
				"analytics",
				"ai_mcp",
				"advanced_kill_switch",
				"multi_broker_restructure",
				"websocket_postback",
			},
		},
		Enums: enumMetadata{
			Sides:         []string{"BUY", "SELL"},
			Products:      []string{"MIS", "NRML", "CNC"},
			OrderTypes:    []string{"MARKET", "LIMIT", "SL"},
			OrderStatuses: []string{trading.OrderStatusOpen, trading.OrderStatusComplete, trading.OrderStatusCancelled, trading.OrderStatusRejected},
			TradeStatuses: []string{trading.TradeStatusOpen, trading.TradeStatusClosed},
			CreationSources: []string{
				trading.CreationSourceLocalSystem,
				trading.CreationSourceKiteApp,
				trading.CreationSourceUnknown,
				trading.CreationSourceMixed,
			},
			ManagementStatuses: []string{
				trading.ManagementStatusManaged,
				trading.ManagementStatusUnmanaged,
				trading.ManagementStatusPartiallyManaged,
				trading.ManagementStatusConflict,
			},
			ExitReasons: []string{
				trading.ExitReasonManual,
				trading.ExitReasonManualExternal,
				trading.ExitReasonStopLoss,
				trading.ExitReasonTarget,
				trading.ExitReasonBothCompleted,
				trading.ExitReasonEntryCancelled,
			},
			Warnings: []string{
				trading.WarningOppositeExposureAcrossProducts,
				trading.WarningPartialExternalExit,
				trading.WarningPositionQuantityMismatch,
				trading.WarningPositionMissingFromBroker,
				trading.WarningProductConversionDetected,
				trading.WarningAmbiguousExternalStopLoss,
				trading.WarningAmbiguousExternalTarget,
			},
			ExternalExitRoles: []string{"stop_loss", "target"},
			RiskStatuses:      []string{"OK", "WARNING", "CONFLICT"},
		},
		Capabilities: capabilityMetadata{
			PaperBroker:               true,
			KiteBroker:                true,
			KiteAuth:                  true,
			KiteSync:                  true,
			OrderEntry:                true,
			OrderbookCancel:           true,
			TradeLevelProtection:      true,
			GroupLevelSingleTradeOps:  true,
			ExternalPositionTakeover:  true,
			ExternalExitManualLinking: true,
			PollingOCO:                true,
			KillSwitch:                false,
			DatabasePersistence:       false,
			Analytics:                 false,
		},
		Endpoints: endpointMap(),
	}
}

func symbolWatchlistMetadata(items []config.SymbolWatchItem) []symbolMetadata {
	if len(items) == 0 {
		return nil
	}
	out := make([]symbolMetadata, 0, len(items))
	for _, item := range items {
		out = append(out, symbolMetadata{
			Exchange:        item.Exchange,
			TradingSymbol:   item.TradingSymbol,
			Product:         item.Product,
			Name:            item.Name,
			DefaultQuantity: item.DefaultQuantity,
			LotSize:         item.LotSize,
			TickSize:        item.TickSize,
		})
	}
	return out
}

func brokerName() string {
	broker := os.Getenv("BROKER")
	if broker == "" {
		return "paper"
	}
	return broker
}

func endpointMap() map[string][]string {
	return map[string][]string{
		"/healthz":                              {"GET"},
		"/metadata":                             {"GET"},
		"/openapi.json":                         {"GET"},
		"/dashboard":                            {"GET"},
		"/conflicts":                            {"GET"},
		"/sync/kite":                            {"POST"},
		"/instruments/sync":                     {"POST"},
		"/instruments/underlyings":              {"GET"},
		"/instruments/expiries":                 {"GET"},
		"/instruments/options":                  {"GET"},
		"/market/ltp":                           {"GET"},
		"/kite/login":                           {"GET"},
		"/kite/callback":                        {"GET"},
		"/trades":                               {"GET", "POST"},
		"/trades/import":                        {"POST"},
		"/trades/{id}":                          {"GET"},
		"/trades/{id}/stop-loss":                {"POST", "DELETE"},
		"/trades/{id}/target":                   {"POST", "DELETE"},
		"/trades/{id}/exit":                     {"POST"},
		"/trades/{id}/exit/amo":                 {"POST"},
		"/trades/{id}/cancel-entry":             {"POST"},
		"/trades/{id}/product-conversion/apply": {"POST"},
		"/groups":                               {"GET"},
		"/groups/{id}":                          {"GET"},
		"/groups/{id}/stop-loss":                {"POST", "DELETE"},
		"/groups/{id}/target":                   {"POST", "DELETE"},
		"/groups/{id}/external-exit/link":       {"POST", "DELETE"},
		"/groups/{id}/exit":                     {"POST"},
		"/groups/{id}/exit/amo":                 {"POST"},
		"/groups/{id}/cancel-entry":             {"POST"},
		"/groups/{id}/take-over":                {"POST"},
		"/orders":                               {"GET"},
		"/orders/{id}":                          {"GET"},
		"/orders/{id}/cancel":                   {"POST"},
		"/positions":                            {"GET"},
		"/positions/{id}":                       {"GET"},
	}
}
