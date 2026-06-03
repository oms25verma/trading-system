package main

import (
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
