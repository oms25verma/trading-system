package main

import "strings"

func openAPISpec() map[string]any {
	paths := make(map[string]any)
	for path, methods := range endpointMap() {
		pathItem := make(map[string]any)
		for _, method := range methods {
			pathItem[strings.ToLower(method)] = map[string]any{
				"summary":     method + " " + path,
				"operationId": operationID(method, path),
				"responses": map[string]any{
					"200": map[string]any{"description": "Success"},
					"400": map[string]any{"description": "Validation error"},
					"404": map[string]any{"description": "Not found"},
					"409": map[string]any{"description": "Conflict or closed trade"},
					"502": map[string]any{"description": "Broker error"},
				},
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
			"schemas": map[string]any{
				"ApiError": map[string]any{
					"type":     "object",
					"required": []string{"kind", "code", "message"},
					"properties": map[string]any{
						"kind":    map[string]string{"type": "string"},
						"code":    map[string]string{"type": "string"},
						"message": map[string]string{"type": "string"},
					},
				},
			},
		},
	}
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
