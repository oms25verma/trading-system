package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"trading-system/internal/config"
	"trading-system/internal/kite"
	"trading-system/internal/trading"
)

func TestTradesListWithoutQueryKeepsArrayResponse(t *testing.T) {
	manager := newHTTPTestManager(t)
	handler := routes(manager, kite.NewClient("", "", ""), config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/trades", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var trades []trading.ManagedTrade
	if err := json.NewDecoder(rec.Body).Decode(&trades); err != nil {
		t.Fatal(err)
	}
	if len(trades) != 2 {
		t.Fatalf("expected 2 trades, got %d", len(trades))
	}
}

func TestTradesListWithQueryFiltersAndPaginates(t *testing.T) {
	manager := newHTTPTestManager(t)
	handler := routes(manager, kite.NewClient("", "", ""), config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/trades?side=BUY&page=1&page_size=1&sort_by=tradingsymbol", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Data       []trading.ManagedTrade `json:"data"`
		Pagination struct {
			Page       int  `json:"page"`
			PageSize   int  `json:"page_size"`
			Total      int  `json:"total"`
			TotalPages int  `json:"total_pages"`
			HasNext    bool `json:"has_next"`
			HasPrev    bool `json:"has_prev"`
		} `json:"pagination"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data) != 1 {
		t.Fatalf("expected 1 trade page, got %d", len(response.Data))
	}
	if response.Data[0].TradingSymbol != "GOLDM26JUNFUT" {
		t.Fatalf("expected GOLDM first after filter/sort, got %s", response.Data[0].TradingSymbol)
	}
	if response.Pagination.Page != 1 || response.Pagination.PageSize != 1 || response.Pagination.Total != 1 || response.Pagination.TotalPages != 1 {
		t.Fatalf("unexpected pagination: %+v", response.Pagination)
	}
	if response.Pagination.HasNext || response.Pagination.HasPrev {
		t.Fatalf("unexpected next/prev flags: %+v", response.Pagination)
	}
}

func TestTradesListRejectsInvalidPagination(t *testing.T) {
	manager := newHTTPTestManager(t)
	handler := routes(manager, kite.NewClient("", "", ""), config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/trades?page=0", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDashboardRouteReturnsSummary(t *testing.T) {
	manager := newHTTPTestManager(t)
	handler := routes(manager, kite.NewClient("", "", ""), config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var summary trading.DashboardSummary
	if err := json.NewDecoder(rec.Body).Decode(&summary); err != nil {
		t.Fatal(err)
	}
	if summary.RiskStatus != "OK" || summary.ActiveGroups != 2 || summary.OpenTrades != 2 {
		t.Fatalf("unexpected dashboard summary: %+v", summary)
	}
}

func TestMetadataRouteReturnsRuntimeDefaultsAndEnums(t *testing.T) {
	manager := newHTTPTestManager(t)
	cfg := config.Config{
		Addr:                    ":9090",
		TradeStorePath:          "data",
		SymbolWatchlist:         []config.SymbolWatchItem{{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS"}},
		DefaultProduct:          "MIS",
		DefaultQuantity:         2,
		DefaultMarketProtection: intPtr(5),
		DefaultStopLossPoints:   10,
		DefaultTargetPoints:     20,
		DefaultSLLimitOffset:    1,
		LogLevel:                "debug",
	}
	handler := routes(manager, kite.NewClient("", "", ""), cfg)

	req := httptest.NewRequest(http.MethodGet, "/metadata", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var metadata metadataResponse
	if err := json.NewDecoder(rec.Body).Decode(&metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Runtime.DefaultQuantity != 2 || metadata.Runtime.DefaultMarketProtection == nil || *metadata.Runtime.DefaultMarketProtection != 5 {
		t.Fatalf("unexpected runtime defaults: %+v", metadata.Runtime)
	}
	if len(metadata.Runtime.SymbolWatchlist) != 1 || metadata.Runtime.SymbolWatchlist[0].TradingSymbol != "INFY" {
		t.Fatalf("expected symbol watchlist in metadata, got %+v", metadata.Runtime.SymbolWatchlist)
	}
	if len(metadata.Enums.Sides) == 0 || len(metadata.Endpoints["/trades"]) == 0 {
		t.Fatalf("expected enums and endpoints, got %+v", metadata)
	}
}

func TestOpenAPIRouteReturnsPathMap(t *testing.T) {
	manager := newHTTPTestManager(t)
	handler := routes(manager, kite.NewClient("", "", ""), config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var spec map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&spec); err != nil {
		t.Fatal(err)
	}
	if spec["openapi"] != "3.0.3" {
		t.Fatalf("unexpected openapi version: %+v", spec["openapi"])
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok || paths["/trades"] == nil || paths["/orders/{id}/cancel"] == nil {
		t.Fatalf("expected path map, got %+v", spec["paths"])
	}
}

func TestOpenAPISpecIncludesAllMetadataEndpoints(t *testing.T) {
	paths := openAPISpec()["paths"].(map[string]any)
	for endpoint := range endpointMap() {
		if paths[endpoint] == nil {
			t.Fatalf("expected openapi spec to include %s", endpoint)
		}
	}
}

func TestCreateTradeRejectsSymbolOutsideWatchlist(t *testing.T) {
	manager := trading.NewManager(trading.NewPaperBroker())
	cfg := config.Config{
		DefaultProduct:         "MIS",
		DefaultQuantity:        1,
		EnforceSymbolWatchlist: true,
		SymbolWatchlist: []config.SymbolWatchItem{
			{Exchange: "NSE", TradingSymbol: "INFY", Product: "MIS"},
		},
	}
	handler := routes(manager, kite.NewClient("", "", ""), cfg)

	body := []byte(`{"exchange":"NSE","tradingsymbol":"RELIANCE","side":"BUY","quantity":1,"product":"MIS","order_type":"MARKET"}`)
	req := httptest.NewRequest(http.MethodPost, "/trades", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var response apiError
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Code != "symbol_not_allowed" {
		t.Fatalf("unexpected error: %+v", response)
	}
}

func TestCreateTradeRequiresConfiguredProtection(t *testing.T) {
	manager := trading.NewManager(trading.NewPaperBroker())
	cfg := config.Config{
		DefaultProduct:         "MIS",
		DefaultQuantity:        1,
		RequireOrderProtection: true,
		DefaultStopLossPoints:  10,
		DefaultTargetPoints:    20,
		DefaultSLLimitOffset:   1,
	}
	handler := routes(manager, kite.NewClient("", "", ""), cfg)

	body := []byte(`{"exchange":"NSE","tradingsymbol":"INFY","side":"BUY","quantity":1,"product":"MIS","order_type":"MARKET"}`)
	req := httptest.NewRequest(http.MethodPost, "/trades", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var response apiError
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Code != "protection_required" {
		t.Fatalf("unexpected error: %+v", response)
	}
}

func TestConflictsRouteReturnsAttentionGroups(t *testing.T) {
	manager := newHTTPTestManager(t)
	handler := routes(manager, kite.NewClient("", "", ""), config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/conflicts", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var conflicts []trading.PositionGroup
	if err := json.NewDecoder(rec.Body).Decode(&conflicts); err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflicts in fixture, got %+v", conflicts)
	}
}

func TestTradeDetailRouteReturnsTrade(t *testing.T) {
	manager := newHTTPTestManager(t)
	handler := routes(manager, kite.NewClient("", "", ""), config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/trades/gold-buy", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var trade trading.ManagedTrade
	if err := json.NewDecoder(rec.Body).Decode(&trade); err != nil {
		t.Fatal(err)
	}
	if trade.ID != "gold-buy" || trade.TradingSymbol != "GOLDM26JUNFUT" {
		t.Fatalf("unexpected trade detail: %+v", trade)
	}
}

func TestGroupDetailRouteReturnsGroup(t *testing.T) {
	manager := newHTTPTestManager(t)
	handler := routes(manager, kite.NewClient("", "", ""), config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/groups/MCX:GOLDM26JUNFUT:MIS", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var group trading.PositionGroup
	if err := json.NewDecoder(rec.Body).Decode(&group); err != nil {
		t.Fatal(err)
	}
	if group.ID != "MCX:GOLDM26JUNFUT:MIS" || group.Quantity != 1 {
		t.Fatalf("unexpected group detail: %+v", group)
	}
}

func TestDetailRouteReturnsNotFound(t *testing.T) {
	manager := newHTTPTestManager(t)
	handler := routes(manager, kite.NewClient("", "", ""), config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/trades/missing", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func newHTTPTestManager(t *testing.T) *trading.Manager {
	t.Helper()
	manager := trading.NewManager(trading.NewPaperBroker())
	imports := []trading.ImportTradeRequest{
		{
			ID:            "silver-sell",
			Exchange:      "MCX",
			TradingSymbol: "SILVERM26JUNFUT",
			Side:          "SELL",
			Quantity:      1,
			Product:       "MIS",
			EntryOrderID:  "entry-1",
		},
		{
			ID:            "gold-buy",
			Exchange:      "MCX",
			TradingSymbol: "GOLDM26JUNFUT",
			Side:          "BUY",
			Quantity:      1,
			Product:       "MIS",
			EntryOrderID:  "entry-2",
		},
	}
	for _, req := range imports {
		if _, err := manager.Import(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	return manager
}

func intPtr(value int) *int {
	return &value
}
