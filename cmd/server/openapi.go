package main

import "strings"

func openAPISpec() map[string]any {
	paths := make(map[string]any)
	for path, methods := range endpointMap() {
		pathItem := make(map[string]any)
		for _, method := range methods {
			operation := map[string]any{
				"summary":     method + " " + path,
				"operationId": operationID(method, path),
				"responses":   responseMap(responseSchema(method, path)),
			}
			if strings.Contains(path, "{id}") {
				operation["parameters"] = []map[string]any{pathParameter("id")}
			}
			if schema := requestSchema(method, path); schema != "" {
				operation["requestBody"] = jsonRequestBody(schema)
			}
			pathItem[strings.ToLower(method)] = map[string]any{
				"summary":     operation["summary"],
				"operationId": operation["operationId"],
				"responses":   operation["responses"],
			}
			if operation["parameters"] != nil {
				pathItem[strings.ToLower(method)].(map[string]any)["parameters"] = operation["parameters"]
			}
			if operation["requestBody"] != nil {
				pathItem[strings.ToLower(method)].(map[string]any)["requestBody"] = operation["requestBody"]
			}
		}
		paths[toOpenAPIPath(path)] = pathItem
	}

	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "Trading System API",
			"version":     "0.1.0",
			"description": "Starter API for local trade management, Kite sync, SL/target/OCO, and frontend dashboard workflows.",
		},
		"servers": []map[string]string{{"url": "http://localhost:8080"}},
		"paths":   paths,
		"components": map[string]any{
			"schemas": componentSchemas(),
		},
	}
}

func responseMap(schema string) map[string]any {
	responses := map[string]any{
		"400": map[string]any{"description": "Validation error", "content": jsonContent("ApiError")},
		"404": map[string]any{"description": "Not found", "content": jsonContent("ApiError")},
		"409": map[string]any{"description": "Conflict or closed trade", "content": jsonContent("ApiError")},
		"502": map[string]any{"description": "Broker error", "content": jsonContent("ApiError")},
	}
	if schema == "" {
		responses["200"] = map[string]any{"description": "Success"}
		return responses
	}
	responses["200"] = map[string]any{"description": "Success", "content": jsonContent(schema)}
	return responses
}

func jsonRequestBody(schema string) map[string]any {
	return map[string]any{
		"required": true,
		"content":  jsonContent(schema),
	}
}

func jsonContent(schema string) map[string]any {
	return map[string]any{
		"application/json": map[string]any{
			"schema": schemaRef(schema),
		},
	}
}

func schemaRef(schema string) map[string]string {
	return map[string]string{"$ref": "#/components/schemas/" + schema}
}

func pathParameter(name string) map[string]any {
	return map[string]any{
		"name":     name,
		"in":       "path",
		"required": true,
		"schema":   map[string]string{"type": "string"},
	}
}

func requestSchema(method, path string) string {
	if method == "POST" && path == "/trades" {
		return "CreateTradeRequest"
	}
	if method == "POST" && path == "/trades/import" {
		return "ImportTradeRequest"
	}
	if method == "POST" && strings.HasSuffix(path, "/stop-loss") {
		return "StopLossRequest"
	}
	if method == "POST" && strings.HasSuffix(path, "/target") {
		return "TargetRequest"
	}
	if method == "POST" && path == "/groups/{id}/external-exit/link" {
		return "ExternalExitLinkRequest"
	}
	if method == "DELETE" && path == "/groups/{id}/external-exit/link" {
		return "ExternalExitLinkRequest"
	}
	if method == "POST" && path == "/groups/{id}/take-over" {
		return "TakeOverGroupRequest"
	}
	if method == "POST" && path == "/historical/sync" {
		return "HistoricalSyncRequest"
	}
	if method == "POST" && path == "/backtests" {
		return "BacktestRequest"
	}
	return ""
}

func responseSchema(method, path string) string {
	switch {
	case path == "/metadata":
		return "MetadataResponse"
	case path == "/dashboard":
		return "DashboardSummary"
	case path == "/sync/kite":
		return "SyncResult"
	case path == "/historical/sync":
		return "HistoricalSyncResult"
	case path == "/historical/candles":
		return "CandleList"
	case path == "/historical/instruments":
		return "HistoricalInstrumentList"
	case path == "/backtests" && method == "POST":
		return "BacktestResult"
	case path == "/backtests" && method == "GET":
		return "BacktestSummaryList"
	case path == "/backtests/{id}":
		return "BacktestResult"
	case path == "/trades" && method == "GET":
		return "ManagedTradeList"
	case strings.HasPrefix(path, "/trades"):
		return "ManagedTrade"
	case path == "/groups" || path == "/conflicts" || path == "/market/groups":
		return "PositionGroupList"
	case path == "/groups/{id}":
		return "PositionGroup"
	case strings.HasPrefix(path, "/groups"):
		return "ManagedTrade"
	case path == "/orders" && method == "GET":
		return "KiteOrderList"
	case path == "/orders/{id}":
		return "KiteOrder"
	case path == "/orders/{id}/cancel":
		return "OrderCancelResult"
	case path == "/positions" && method == "GET":
		return "KitePositionList"
	case path == "/positions/live":
		return "KitePositionList"
	case path == "/positions/{id}":
		return "KitePosition"
	default:
		return ""
	}
}

func componentSchemas() map[string]any {
	return map[string]any{
		"ApiError": objectSchema([]string{"kind", "code", "message"}, map[string]any{
			"kind":    stringSchema(),
			"code":    stringSchema(),
			"message": stringSchema(),
		}),
		"CreateTradeRequest": objectSchema([]string{"exchange", "tradingsymbol", "side", "quantity", "product", "order_type"}, map[string]any{
			"exchange":          stringSchema(),
			"tradingsymbol":     stringSchema(),
			"side":              enumSchema("BUY", "SELL"),
			"quantity":          integerSchema(),
			"product":           stringSchema(),
			"order_type":        enumSchema("MARKET", "LIMIT"),
			"price":             numberSchema(),
			"market_protection": integerSchema(),
			"protection":        schemaRef("AutoProtectionRequest"),
		}),
		"AutoProtectionRequest": objectSchema(nil, map[string]any{
			"reference_price":  numberSchema(),
			"stop_loss_points": numberSchema(),
			"target_points":    numberSchema(),
			"trail_by":         numberSchema(),
			"sl_limit_offset":  numberSchema(),
		}),
		"ImportTradeRequest": objectSchema([]string{"exchange", "tradingsymbol", "side", "quantity", "product", "entry_order_id"}, map[string]any{
			"id":             stringSchema(),
			"exchange":       stringSchema(),
			"tradingsymbol":  stringSchema(),
			"side":           enumSchema("BUY", "SELL"),
			"quantity":       integerSchema(),
			"product":        stringSchema(),
			"entry_price":    numberSchema(),
			"entry_order_id": stringSchema(),
		}),
		"StopLossRequest": objectSchema([]string{"trigger_price", "limit_price"}, map[string]any{
			"trigger_price": numberSchema(),
			"limit_price":   numberSchema(),
			"trail_by":      numberSchema(),
		}),
		"TargetRequest": objectSchema([]string{"price"}, map[string]any{"price": numberSchema()}),
		"ExternalExitLinkRequest": objectSchema([]string{"role"}, map[string]any{
			"order_id": stringSchema(),
			"role":     enumSchema("stop_loss", "target"),
		}),
		"TakeOverGroupRequest": objectSchema(nil, map[string]any{"entry_price": numberSchema()}),
		"HistoricalSyncRequest": objectSchema([]string{"exchange", "tradingsymbol", "instrument_token", "interval", "from", "to"}, map[string]any{
			"exchange":         stringSchema(),
			"tradingsymbol":    stringSchema(),
			"instrument_token": integerSchema(),
			"interval":         enumSchema("minute", "3minute", "5minute", "10minute", "15minute", "30minute", "60minute", "day"),
			"from":             stringSchema(),
			"to":               stringSchema(),
			"continuous":       boolSchema(),
			"include_oi":       boolSchema(),
		}),
		"HistoricalSyncResult": objectSchema(nil, map[string]any{
			"exchange":         stringSchema(),
			"tradingsymbol":    stringSchema(),
			"instrument_token": integerSchema(),
			"interval":         stringSchema(),
			"from":             stringSchema(),
			"to":               stringSchema(),
			"candles_fetched":  integerSchema(),
			"candles_stored":   integerSchema(),
			"path":             stringSchema(),
			"paths":            arraySchema(stringSchema()),
			"synced_at":        stringSchema(),
		}),
		"HistoricalInstrument": objectSchema(nil, map[string]any{
			"instrument_token": integerSchema(),
			"exchange":         stringSchema(),
			"tradingsymbol":    stringSchema(),
			"underlying":       stringSchema(),
			"expiry":           stringSchema(),
			"strike":           numberSchema(),
			"instrument_type":  stringSchema(),
			"segment":          stringSchema(),
			"lot_size":         integerSchema(),
			"tick_size":        numberSchema(),
		}),
		"Candle": objectSchema(nil, map[string]any{
			"time":   stringSchema(),
			"open":   numberSchema(),
			"high":   numberSchema(),
			"low":    numberSchema(),
			"close":  numberSchema(),
			"volume": integerSchema(),
			"oi":     integerSchema(),
		}),
		"BacktestRequest": objectSchema([]string{"exchange", "tradingsymbol", "interval", "from", "to", "strategy"}, map[string]any{
			"exchange":            stringSchema(),
			"tradingsymbol":       stringSchema(),
			"interval":            enumSchema("minute", "3minute", "5minute", "10minute", "15minute", "30minute", "60minute", "day"),
			"from":                stringSchema(),
			"to":                  stringSchema(),
			"strategy":            enumSchema("opening_range_breakout", "orb", "none"),
			"quantity":            integerSchema(),
			"multiplier":          numberSchema(),
			"stop_loss_points":    numberSchema(),
			"target_points":       numberSchema(),
			"entry_buffer_points": numberSchema(),
			"slippage_points":     numberSchema(),
			"brokerage_per_trade": numberSchema(),
			"range_start":         stringSchema(),
			"range_end":           stringSchema(),
			"exit_time":           stringSchema(),
		}),
		"BacktestTrade": objectSchema(nil, map[string]any{
			"entry_time": stringSchema(),
			"exit_time":  stringSchema(),
			"side":       stringSchema(),
			"entry":      numberSchema(),
			"exit":       numberSchema(),
			"quantity":   integerSchema(),
			"gross_pnl":  numberSchema(),
			"costs":      numberSchema(),
			"pnl":        numberSchema(),
			"reason":     stringSchema(),
		}),
		"EquityPoint": objectSchema(nil, map[string]any{
			"time":     stringSchema(),
			"equity":   numberSchema(),
			"drawdown": numberSchema(),
		}),
		"BacktestResult": objectSchema(nil, map[string]any{
			"id":            stringSchema(),
			"strategy":      stringSchema(),
			"exchange":      stringSchema(),
			"tradingsymbol": stringSchema(),
			"interval":      stringSchema(),
			"trades":        arraySchema(schemaRef("BacktestTrade")),
			"total_pnl":     numberSchema(),
			"gross_pnl":     numberSchema(),
			"total_costs":   numberSchema(),
			"max_drawdown":  numberSchema(),
			"win_rate":      numberSchema(),
			"expectancy":    numberSchema(),
			"avg_win":       numberSchema(),
			"avg_loss":      numberSchema(),
			"equity_curve":  arraySchema(schemaRef("EquityPoint")),
			"candles_used":  integerSchema(),
			"generated_at":  stringSchema(),
		}),
		"BacktestSummary": objectSchema(nil, map[string]any{
			"id":            stringSchema(),
			"strategy":      stringSchema(),
			"exchange":      stringSchema(),
			"tradingsymbol": stringSchema(),
			"interval":      stringSchema(),
			"trades":        integerSchema(),
			"total_pnl":     numberSchema(),
			"gross_pnl":     numberSchema(),
			"total_costs":   numberSchema(),
			"max_drawdown":  numberSchema(),
			"win_rate":      numberSchema(),
			"expectancy":    numberSchema(),
			"generated_at":  stringSchema(),
		}),
		"ManagedTrade": objectSchema(nil, map[string]any{
			"id":                  stringSchema(),
			"exchange":            stringSchema(),
			"tradingsymbol":       stringSchema(),
			"side":                stringSchema(),
			"quantity":            integerSchema(),
			"initial_quantity":    integerSchema(),
			"product":             stringSchema(),
			"entry_price":         numberSchema(),
			"entry_order_id":      stringSchema(),
			"entry_status":        stringSchema(),
			"trade_status":        stringSchema(),
			"creation_source":     stringSchema(),
			"exit_reason":         stringSchema(),
			"exit_order_id":       stringSchema(),
			"stop_order_id":       stringSchema(),
			"stop_order_status":   stringSchema(),
			"target_order_id":     stringSchema(),
			"target_order_status": stringSchema(),
			"stop_loss":           schemaRef("StopLoss"),
			"target":              schemaRef("Target"),
			"created_at":          dateTimeSchema(),
			"updated_at":          dateTimeSchema(),
			"closed_at":           dateTimeSchema(),
		}),
		"StopLoss": objectSchema(nil, map[string]any{
			"trigger_price": numberSchema(),
			"limit_price":   numberSchema(),
			"trail_by":      numberSchema(),
			"highest_ltp":   numberSchema(),
			"lowest_ltp":    numberSchema(),
		}),
		"Target": objectSchema(nil, map[string]any{"price": numberSchema()}),
		"PositionGroup": objectSchema(nil, map[string]any{
			"id":                     stringSchema(),
			"exchange":               stringSchema(),
			"tradingsymbol":          stringSchema(),
			"product":                stringSchema(),
			"side":                   stringSchema(),
			"quantity":               integerSchema(),
			"local_quantity":         integerSchema(),
			"broker_quantity":        integerSchema(),
			"average_entry_price":    numberSchema(),
			"last_price":             numberSchema(),
			"unrealized_pnl":         numberSchema(),
			"pnl_percent":            numberSchema(),
			"market_synced_at":       dateTimeSchema(),
			"trade_ids":              arraySchema(map[string]string{"type": "string"}),
			"trade_status":           stringSchema(),
			"creation_source":        stringSchema(),
			"management_status":      stringSchema(),
			"stop_loss_count":        integerSchema(),
			"target_count":           integerSchema(),
			"stop_loss":              schemaRef("StopLoss"),
			"target":                 schemaRef("Target"),
			"exit_order_id":          stringSchema(),
			"exit_pending":           boolSchema(),
			"converted_from_product": stringSchema(),
			"converted_to_product":   stringSchema(),
			"warnings":               arraySchema(map[string]string{"type": "string"}),
			"created_at":             dateTimeSchema(),
			"updated_at":             dateTimeSchema(),
		}),
		"KiteOrder": objectSchema(nil, map[string]any{
			"order_id":          stringSchema(),
			"exchange":          stringSchema(),
			"tradingsymbol":     stringSchema(),
			"transaction_type":  stringSchema(),
			"quantity":          integerSchema(),
			"filled_quantity":   integerSchema(),
			"pending_quantity":  integerSchema(),
			"product":           stringSchema(),
			"order_type":        stringSchema(),
			"variety":           stringSchema(),
			"validity":          stringSchema(),
			"status":            stringSchema(),
			"status_message":    stringSchema(),
			"exchange_order_id": stringSchema(),
			"price":             numberSchema(),
			"trigger_price":     numberSchema(),
			"average_price":     numberSchema(),
			"tag":               stringSchema(),
			"creation_source":   stringSchema(),
			"order_timestamp":   dateTimeSchema(),
			"synced_at":         dateTimeSchema(),
		}),
		"KitePosition": objectSchema(nil, map[string]any{
			"exchange":      stringSchema(),
			"tradingsymbol": stringSchema(),
			"product":       stringSchema(),
			"quantity":      integerSchema(),
			"average_price": numberSchema(),
			"last_price":    numberSchema(),
			"pnl":           numberSchema(),
			"synced_at":     dateTimeSchema(),
		}),
		"SyncResult": objectSchema(nil, map[string]any{
			"synced_at":                    dateTimeSchema(),
			"orders_synced":                integerSchema(),
			"positions_synced":             integerSchema(),
			"positions_added":              integerSchema(),
			"positions_changed":            integerSchema(),
			"positions_removed":            integerSchema(),
			"product_conversions_detected": integerSchema(),
			"external_stop_losses_linked":  integerSchema(),
			"external_targets_linked":      integerSchema(),
			"ambiguous_external_exits":     integerSchema(),
			"local_system_orders":          integerSchema(),
			"external_orders":              integerSchema(),
		}),
		"DashboardSummary": objectSchema(nil, map[string]any{
			"risk_status":      stringSchema(),
			"active_groups":    integerSchema(),
			"managed_groups":   integerSchema(),
			"unmanaged_groups": integerSchema(),
			"conflict_groups":  integerSchema(),
			"warning_groups":   integerSchema(),
			"open_trades":      integerSchema(),
			"closed_trades":    integerSchema(),
			"open_orders":      integerSchema(),
			"rejected_orders":  integerSchema(),
			"synced_orders":    integerSchema(),
			"synced_positions": integerSchema(),
		}),
		"MetadataResponse": objectSchema(nil, map[string]any{}),
		"OrderCancelResult": objectSchema(nil, map[string]any{
			"order": schemaRef("KiteOrder"),
			"trade": schemaRef("ManagedTrade"),
		}),
		"ManagedTradeList":         arraySchema(schemaRef("ManagedTrade")),
		"PositionGroupList":        arraySchema(schemaRef("PositionGroup")),
		"KiteOrderList":            arraySchema(schemaRef("KiteOrder")),
		"KitePositionList":         arraySchema(schemaRef("KitePosition")),
		"CandleList":               arraySchema(schemaRef("Candle")),
		"HistoricalInstrumentList": arraySchema(schemaRef("HistoricalInstrument")),
		"BacktestSummaryList":      arraySchema(schemaRef("BacktestSummary")),
	}
}

func objectSchema(required []string, properties map[string]any) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func arraySchema(items any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}

func stringSchema() map[string]string {
	return map[string]string{"type": "string"}
}

func dateTimeSchema() map[string]string {
	return map[string]string{"type": "string", "format": "date-time"}
}

func numberSchema() map[string]string {
	return map[string]string{"type": "number", "format": "double"}
}

func integerSchema() map[string]string {
	return map[string]string{"type": "integer"}
}

func boolSchema() map[string]string {
	return map[string]string{"type": "boolean"}
}

func enumSchema(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}

func toOpenAPIPath(path string) string {
	if strings.Contains(path, "{id}") || strings.Contains(path, "{order_id}") {
		return path
	}
	return path
}

func operationID(method, path string) string {
	cleaned := strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_").Replace(path)
	cleaned = strings.Trim(cleaned, "_")
	if cleaned == "" {
		cleaned = "root"
	}
	return strings.ToLower(method) + "_" + cleaned
}
