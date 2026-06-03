package main

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"trading-system/internal/trading"
)

const (
	defaultPageSize = 50
	maxPageSize     = 500
)

type paginatedResponse struct {
	Data       any            `json:"data"`
	Pagination paginationMeta `json:"pagination"`
}

type paginationMeta struct {
	Page       int  `json:"page"`
	PageSize   int  `json:"page_size"`
	Total      int  `json:"total"`
	TotalPages int  `json:"total_pages"`
	HasNext    bool `json:"has_next"`
	HasPrev    bool `json:"has_prev"`
}

func hasListQuery(r *http.Request) bool {
	return len(r.URL.Query()) > 0
}

func writePagedResult[T any](ctx context.Context, w http.ResponseWriter, r *http.Request, data []T) {
	page, pageSize, err := parsePagination(r)
	if err != nil {
		writeError(ctx, w, err)
		return
	}
	paged, meta := paginate(data, page, pageSize)
	writeJSON(w, http.StatusOK, paginatedResponse{
		Data:       paged,
		Pagination: meta,
	})
}

func parsePagination(r *http.Request) (int, int, error) {
	page, err := positiveIntQuery(r, "page", 1)
	if err != nil {
		return 0, 0, err
	}
	pageSize, err := positiveIntQuery(r, "page_size", defaultPageSize)
	if err != nil {
		return 0, 0, err
	}
	if pageSize > maxPageSize {
		return 0, 0, &trading.DomainError{Kind: trading.ErrorKindValidation, Code: "page_size_too_large", Message: "page_size must be 500 or less"}
	}
	return page, pageSize, nil
}

func positiveIntQuery(r *http.Request, name string, fallback int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, &trading.DomainError{Kind: trading.ErrorKindValidation, Code: "invalid_" + name, Message: name + " must be a positive integer"}
	}
	return value, nil
}

func paginate[T any](data []T, page, pageSize int) ([]T, paginationMeta) {
	total := len(data)
	totalPages := 0
	if total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	start := (page - 1) * pageSize
	if start >= total {
		return []T{}, paginationMeta{Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages, HasPrev: page > 1}
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return data[start:end], paginationMeta{
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages,
		HasNext:    end < total,
		HasPrev:    page > 1,
	}
}

func filterTrades(trades []*trading.ManagedTrade, r *http.Request) []*trading.ManagedTrade {
	out := make([]*trading.ManagedTrade, 0, len(trades))
	for _, trade := range trades {
		if trade == nil {
			continue
		}
		if !queryEqual(r, "exchange", trade.Exchange) ||
			!queryEqualAny(r, []string{"symbol", "tradingsymbol"}, trade.TradingSymbol) ||
			!queryEqual(r, "side", trade.Side) ||
			!queryEqual(r, "product", trade.Product) ||
			!queryEqualAny(r, []string{"status", "trade_status"}, trade.TradeStatus) ||
			!queryEqual(r, "creation_source", trade.CreationSource) {
			continue
		}
		out = append(out, trade)
	}
	sortTrades(out, r)
	return out
}

func sortTrades(trades []*trading.ManagedTrade, r *http.Request) {
	sortBy := strings.ToLower(valueOr(r.URL.Query().Get("sort_by"), "created_at"))
	desc := sortDesc(r)
	sort.SliceStable(trades, func(i, j int) bool {
		cmp := 0
		switch sortBy {
		case "updated_at":
			cmp = compareTime(trades[i].UpdatedAt, trades[j].UpdatedAt)
		case "symbol", "tradingsymbol":
			cmp = strings.Compare(trades[i].TradingSymbol, trades[j].TradingSymbol)
		case "side":
			cmp = strings.Compare(trades[i].Side, trades[j].Side)
		case "product":
			cmp = strings.Compare(trades[i].Product, trades[j].Product)
		case "status", "trade_status":
			cmp = strings.Compare(trades[i].TradeStatus, trades[j].TradeStatus)
		default:
			cmp = compareTime(trades[i].CreatedAt, trades[j].CreatedAt)
		}
		return compareForSort(cmp, desc)
	})
}

func filterGroups(groups []*trading.PositionGroup, r *http.Request) []*trading.PositionGroup {
	out := make([]*trading.PositionGroup, 0, len(groups))
	for _, group := range groups {
		if group == nil {
			continue
		}
		if !queryEqual(r, "exchange", group.Exchange) ||
			!queryEqualAny(r, []string{"symbol", "tradingsymbol"}, group.TradingSymbol) ||
			!queryEqual(r, "side", group.Side) ||
			!queryEqual(r, "product", group.Product) ||
			!queryEqual(r, "creation_source", group.CreationSource) ||
			!queryEqual(r, "management_status", group.ManagementStatus) ||
			!queryContains(r, "warning", group.Warnings) {
			continue
		}
		out = append(out, group)
	}
	sortGroups(out, r)
	return out
}

func sortGroups(groups []*trading.PositionGroup, r *http.Request) {
	sortBy := strings.ToLower(valueOr(r.URL.Query().Get("sort_by"), "id"))
	desc := sortDesc(r)
	sort.SliceStable(groups, func(i, j int) bool {
		cmp := 0
		switch sortBy {
		case "updated_at":
			cmp = compareTime(groups[i].UpdatedAt, groups[j].UpdatedAt)
		case "symbol", "tradingsymbol":
			cmp = strings.Compare(groups[i].TradingSymbol, groups[j].TradingSymbol)
		case "side":
			cmp = strings.Compare(groups[i].Side, groups[j].Side)
		case "quantity":
			cmp = compareInt(groups[i].Quantity, groups[j].Quantity)
		case "management_status":
			cmp = strings.Compare(groups[i].ManagementStatus, groups[j].ManagementStatus)
		default:
			cmp = strings.Compare(groups[i].ID, groups[j].ID)
		}
		return compareForSort(cmp, desc)
	})
}

func filterOrders(orders []*trading.KiteOrder, r *http.Request) []*trading.KiteOrder {
	out := make([]*trading.KiteOrder, 0, len(orders))
	for _, order := range orders {
		if order == nil {
			continue
		}
		if !queryEqual(r, "exchange", order.Exchange) ||
			!queryEqualAny(r, []string{"symbol", "tradingsymbol"}, order.TradingSymbol) ||
			!queryEqualAny(r, []string{"side", "transaction_type"}, order.TransactionType) ||
			!queryEqual(r, "product", order.Product) ||
			!queryEqual(r, "order_type", order.OrderType) ||
			!queryEqual(r, "status", order.Status) ||
			!queryEqual(r, "creation_source", order.CreationSource) {
			continue
		}
		out = append(out, order)
	}
	sortOrders(out, r)
	return out
}

func sortOrders(orders []*trading.KiteOrder, r *http.Request) {
	sortBy := strings.ToLower(valueOr(r.URL.Query().Get("sort_by"), "synced_at"))
	desc := sortDesc(r)
	sort.SliceStable(orders, func(i, j int) bool {
		cmp := 0
		switch sortBy {
		case "symbol", "tradingsymbol":
			cmp = strings.Compare(orders[i].TradingSymbol, orders[j].TradingSymbol)
		case "status":
			cmp = strings.Compare(orders[i].Status, orders[j].Status)
		case "side", "transaction_type":
			cmp = strings.Compare(orders[i].TransactionType, orders[j].TransactionType)
		case "price":
			cmp = compareFloat(orders[i].Price, orders[j].Price)
		case "order_id":
			cmp = strings.Compare(orders[i].OrderID, orders[j].OrderID)
		default:
			cmp = compareTime(orders[i].SyncedAt, orders[j].SyncedAt)
		}
		return compareForSort(cmp, desc)
	})
}

func filterPositions(positions []*trading.KitePosition, r *http.Request) []*trading.KitePosition {
	out := make([]*trading.KitePosition, 0, len(positions))
	for _, position := range positions {
		if position == nil {
			continue
		}
		if !queryEqual(r, "exchange", position.Exchange) ||
			!queryEqualAny(r, []string{"symbol", "tradingsymbol"}, position.TradingSymbol) ||
			!queryEqual(r, "product", position.Product) {
			continue
		}
		out = append(out, position)
	}
	sortPositions(out, r)
	return out
}

func sortPositions(positions []*trading.KitePosition, r *http.Request) {
	sortBy := strings.ToLower(valueOr(r.URL.Query().Get("sort_by"), "synced_at"))
	desc := sortDesc(r)
	sort.SliceStable(positions, func(i, j int) bool {
		cmp := 0
		switch sortBy {
		case "symbol", "tradingsymbol":
			cmp = strings.Compare(positions[i].TradingSymbol, positions[j].TradingSymbol)
		case "quantity":
			cmp = compareInt(positions[i].Quantity, positions[j].Quantity)
		default:
			cmp = compareTime(positions[i].SyncedAt, positions[j].SyncedAt)
		}
		return compareForSort(cmp, desc)
	})
}

func queryEqual(r *http.Request, name, actual string) bool {
	expected := strings.TrimSpace(r.URL.Query().Get(name))
	return expected == "" || strings.EqualFold(expected, actual)
}

func queryEqualAny(r *http.Request, names []string, actual string) bool {
	for _, name := range names {
		expected := strings.TrimSpace(r.URL.Query().Get(name))
		if expected != "" && !strings.EqualFold(expected, actual) {
			return false
		}
	}
	return true
}

func queryContains(r *http.Request, name string, values []string) bool {
	expected := strings.TrimSpace(r.URL.Query().Get(name))
	if expected == "" {
		return true
	}
	for _, value := range values {
		if strings.EqualFold(expected, value) {
			return true
		}
	}
	return false
}

func sortDesc(r *http.Request) bool {
	dir := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort_dir")))
	return dir == "desc" || dir == "descending"
}

func compareForSort(cmp int, desc bool) bool {
	if desc {
		return cmp > 0
	}
	return cmp < 0
}

func compareTime(left, right time.Time) int {
	if left.Before(right) {
		return -1
	}
	if left.After(right) {
		return 1
	}
	return 0
}

func compareInt(left, right int) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func compareFloat(left, right float64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
