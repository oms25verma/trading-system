package kite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"trading-system/internal/observability"
)

const baseURL = "https://api.kite.trade"

type Client struct {
	apiKey      string
	apiSecret   string
	accessToken string
	httpClient  *http.Client
	logger      *slog.Logger
}

type APIError struct {
	Method     string
	Path       string
	StatusCode int
	Status     string
	Message    string
	ErrorType  string
	RawBody    string
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	message := e.Message
	if message == "" {
		message = e.RawBody
	}
	return fmt.Sprintf("kite %s %s failed: status=%d error_type=%s message=%s", e.Method, e.Path, e.StatusCode, valueOr(e.ErrorType, "UNKNOWN"), message)
}

func NewClient(apiKey, apiSecret, accessToken string) *Client {
	return &Client{
		apiKey:      apiKey,
		apiSecret:   apiSecret,
		accessToken: accessToken,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		logger:      slog.Default(),
	}
}

func (c *Client) LoginURL() (string, error) {
	if c.apiKey == "" {
		return "", errors.New("missing KITE_API_KEY")
	}
	return "https://kite.zerodha.com/connect/login?v=3&api_key=" + url.QueryEscape(c.apiKey), nil
}

type Session struct {
	UserID      string `json:"user_id"`
	UserName    string `json:"user_name"`
	APIKey      string `json:"api_key"`
	AccessToken string `json:"access_token"`
	LoginTime   string `json:"login_time"`
}

func (c *Client) GenerateSession(ctx context.Context, requestToken string) (*Session, error) {
	if c.apiKey == "" || c.apiSecret == "" {
		return nil, errors.New("missing KITE_API_KEY or KITE_API_SECRET")
	}
	if requestToken == "" {
		return nil, errors.New("missing request_token")
	}

	form := url.Values{}
	form.Set("api_key", c.apiKey)
	form.Set("request_token", requestToken)
	form.Set("checksum", checksum(c.apiKey, requestToken, c.apiSecret))

	var out struct {
		Data Session `json:"data"`
	}
	if err := c.doPublicForm(ctx, http.MethodPost, "/session/token", form, &out); err != nil {
		return nil, err
	}
	c.accessToken = out.Data.AccessToken
	return &out.Data, nil
}

type OrderRequest struct {
	Exchange         string  `json:"exchange"`
	TradingSymbol    string  `json:"tradingsymbol"`
	TransactionType  string  `json:"transaction_type"`
	Quantity         int     `json:"quantity"`
	Product          string  `json:"product"`
	OrderType        string  `json:"order_type"`
	Variety          string  `json:"variety"`
	Validity         string  `json:"validity"`
	Price            float64 `json:"price,omitempty"`
	TriggerPrice     float64 `json:"trigger_price,omitempty"`
	MarketProtection *int    `json:"market_protection,omitempty"`
	Tag              string  `json:"tag,omitempty"`
}

type OrderResponse struct {
	OrderID string `json:"order_id"`
}

type OrderStatusResponse struct {
	OrderID                 string  `json:"order_id"`
	Exchange                string  `json:"exchange"`
	TradingSymbol           string  `json:"tradingsymbol"`
	TransactionType         string  `json:"transaction_type"`
	Quantity                int     `json:"quantity"`
	Product                 string  `json:"product"`
	OrderType               string  `json:"order_type"`
	Variety                 string  `json:"variety"`
	Validity                string  `json:"validity"`
	Status                  string  `json:"status"`
	StatusMessage           string  `json:"status_message"`
	ExchangeOrderID         string  `json:"exchange_order_id"`
	Price                   float64 `json:"price"`
	TriggerPrice            float64 `json:"trigger_price"`
	AveragePrice            float64 `json:"average_price"`
	FilledQuantity          int     `json:"filled_quantity"`
	PendingQuantity         int     `json:"pending_quantity"`
	Tag                     string  `json:"tag"`
	OrderTimestamp          string  `json:"order_timestamp"`
	ExchangeTimestamp       string  `json:"exchange_timestamp"`
	ExchangeUpdateTimestamp string  `json:"exchange_update_timestamp"`
}

func (c *Client) PlaceOrder(ctx context.Context, order OrderRequest) (string, error) {
	variety := valueOr(order.Variety, "regular")
	form := url.Values{}
	form.Set("exchange", order.Exchange)
	form.Set("tradingsymbol", order.TradingSymbol)
	form.Set("transaction_type", order.TransactionType)
	form.Set("quantity", strconv.Itoa(order.Quantity))
	form.Set("product", valueOr(order.Product, "MIS"))
	form.Set("order_type", valueOr(order.OrderType, "MARKET"))
	form.Set("validity", valueOr(order.Validity, "DAY"))
	setFloat(form, "price", order.Price)
	setFloat(form, "trigger_price", order.TriggerPrice)
	if order.MarketProtection != nil {
		form.Set("market_protection", strconv.Itoa(*order.MarketProtection))
	}
	if order.Tag != "" {
		form.Set("tag", order.Tag)
	}

	var out struct {
		Data OrderResponse `json:"data"`
	}
	if err := c.doForm(ctx, http.MethodPost, "/orders/"+variety, form, &out); err != nil {
		return "", err
	}
	return out.Data.OrderID, nil
}

func (c *Client) ModifyOrder(ctx context.Context, variety, orderID string, fields map[string]string) error {
	form := url.Values{}
	for key, value := range fields {
		form.Set(key, value)
	}
	return c.doForm(ctx, http.MethodPut, "/orders/"+valueOr(variety, "regular")+"/"+orderID, form, nil)
}

func (c *Client) CancelOrder(ctx context.Context, variety, orderID string) error {
	return c.doForm(ctx, http.MethodDelete, "/orders/"+valueOr(variety, "regular")+"/"+orderID, nil, nil)
}

func (c *Client) OrderStatus(ctx context.Context, orderID string) (string, error) {
	details, err := c.OrderDetails(ctx, orderID)
	if err != nil {
		return "", err
	}
	return details.Status, nil
}

func (c *Client) OrderDetails(ctx context.Context, orderID string) (*OrderDetailsResponse, error) {
	var out struct {
		Data []OrderStatusResponse `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/orders/"+orderID, nil, &out); err != nil {
		return nil, err
	}
	if len(out.Data) == 0 {
		return nil, fmt.Errorf("order history not found for %s", orderID)
	}
	latest := out.Data[len(out.Data)-1]
	return &OrderDetailsResponse{
		OrderID:         latest.OrderID,
		Status:          latest.Status,
		Price:           latest.Price,
		TriggerPrice:    latest.TriggerPrice,
		FilledQuantity:  latest.FilledQuantity,
		PendingQuantity: latest.PendingQuantity,
	}, nil
}

func (c *Client) Orders(ctx context.Context) ([]OrderStatusResponse, error) {
	var out struct {
		Data []OrderStatusResponse `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/orders", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

type OrderDetailsResponse struct {
	OrderID         string
	Status          string
	Price           float64
	TriggerPrice    float64
	FilledQuantity  int
	PendingQuantity int
}

type PositionResponse struct {
	Exchange      string `json:"exchange"`
	TradingSymbol string `json:"tradingsymbol"`
	Product       string `json:"product"`
	Quantity      int    `json:"quantity"`
}

func (c *Client) Positions(ctx context.Context) ([]PositionResponse, error) {
	var out struct {
		Data struct {
			Net []PositionResponse `json:"net"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/portfolio/positions", nil, &out); err != nil {
		return nil, err
	}
	return out.Data.Net, nil
}

func (c *Client) LTP(ctx context.Context, exchange, symbol string) (float64, error) {
	instrument := exchange + ":" + symbol
	path := "/quote/ltp?i=" + url.QueryEscape(instrument)
	var out struct {
		Data map[string]struct {
			LastPrice float64 `json:"last_price"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return 0, err
	}
	quote, ok := out.Data[instrument]
	if !ok {
		return 0, fmt.Errorf("ltp not found for %s", instrument)
	}
	return quote.LastPrice, nil
}

func (c *Client) doForm(ctx context.Context, method, path string, form url.Values, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	return c.doAuthenticated(ctx, method, path, body, out, safeFormValues(form))
}

func (c *Client) doPublicForm(ctx context.Context, method, path string, form url.Values, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	return c.doPublic(ctx, method, path, body, out, safeFormValues(form))
}

func (c *Client) doPublic(ctx context.Context, method, path string, body io.Reader, out any, requestFields map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("X-Kite-Version", "3")
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	startedAt := time.Now()
	c.logBrokerRequest(ctx, method, path, requestFields)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logBrokerError(ctx, method, path, time.Since(startedAt), err)
		return err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logBrokerError(ctx, method, path, time.Since(startedAt), err)
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.logBrokerResponse(ctx, method, path, resp.StatusCode, time.Since(startedAt), payload, true)
		return kiteAPIError(method, path, resp.StatusCode, payload)
	}
	c.logBrokerResponse(ctx, method, path, resp.StatusCode, time.Since(startedAt), payload, false)
	if out == nil || len(payload) == 0 {
		return nil
	}
	return json.Unmarshal(payload, out)
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, out any) error {
	return c.doAuthenticated(ctx, method, path, body, out, nil)
}

func (c *Client) doAuthenticated(ctx context.Context, method, path string, body io.Reader, out any, requestFields map[string]string) error {
	if c.apiKey == "" || c.accessToken == "" {
		return errors.New("missing KITE_API_KEY or KITE_ACCESS_TOKEN")
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("X-Kite-Version", "3")
	req.Header.Set("Authorization", "token "+c.apiKey+":"+c.accessToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	startedAt := time.Now()
	c.logBrokerRequest(ctx, method, path, requestFields)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logBrokerError(ctx, method, path, time.Since(startedAt), err)
		return err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logBrokerError(ctx, method, path, time.Since(startedAt), err)
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.logBrokerResponse(ctx, method, path, resp.StatusCode, time.Since(startedAt), payload, true)
		return kiteAPIError(method, path, resp.StatusCode, payload)
	}
	c.logBrokerResponse(ctx, method, path, resp.StatusCode, time.Since(startedAt), payload, false)
	if out == nil || len(payload) == 0 {
		return nil
	}
	return json.Unmarshal(payload, out)
}

func (c *Client) logBrokerRequest(ctx context.Context, method, path string, requestFields map[string]string) {
	if c.logger == nil {
		return
	}
	args := []any{
		"request_id", observability.RequestID(ctx),
		"broker", "kite",
		"method", method,
		"path", path,
	}
	if len(requestFields) > 0 {
		args = append(args, "request_fields", requestFields)
	}
	c.logger.DebugContext(ctx, "broker_request_started", args...)
}

func (c *Client) logBrokerResponse(ctx context.Context, method, path string, statusCode int, duration time.Duration, payload []byte, failed bool) {
	if c.logger == nil {
		return
	}
	args := []any{
		"request_id", observability.RequestID(ctx),
		"broker", "kite",
		"method", method,
		"path", path,
		"status", statusCode,
		"duration_ms", duration.Milliseconds(),
		"response_bytes", len(payload),
	}
	if failed {
		args = append(args, "response_body", safeResponseBody(payload))
		c.logger.WarnContext(ctx, "broker_request_failed", args...)
		return
	}
	c.logger.DebugContext(ctx, "broker_request_finished", args...)
}

func (c *Client) logBrokerError(ctx context.Context, method, path string, duration time.Duration, err error) {
	if c.logger == nil {
		return
	}
	c.logger.WarnContext(ctx, "broker_request_failed",
		"request_id", observability.RequestID(ctx),
		"broker", "kite",
		"method", method,
		"path", path,
		"duration_ms", duration.Milliseconds(),
		"error", err,
	)
}

func safeFormValues(form url.Values) map[string]string {
	if len(form) == 0 {
		return nil
	}
	out := make(map[string]string, len(form))
	for key := range form {
		value := form.Get(key)
		if isSensitiveField(key) {
			value = "[REDACTED]"
		}
		out[key] = value
	}
	return out
}

func isSensitiveField(key string) bool {
	switch strings.ToLower(key) {
	case "api_key", "api_secret", "access_token", "request_token", "checksum", "authorization":
		return true
	default:
		return false
	}
}

func safeResponseBody(payload []byte) string {
	const limit = 500
	body := string(bytes.TrimSpace(payload))
	if len(body) > limit {
		return body[:limit] + "...[truncated]"
	}
	return body
}

func kiteAPIError(method, path string, statusCode int, payload []byte) error {
	body := string(bytes.TrimSpace(payload))
	var parsed struct {
		Status    string          `json:"status"`
		Message   string          `json:"message"`
		ErrorType string          `json:"error_type"`
		Data      json.RawMessage `json:"data"`
	}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &parsed)
	}
	return &APIError{
		Method:     method,
		Path:       path,
		StatusCode: statusCode,
		Status:     parsed.Status,
		Message:    parsed.Message,
		ErrorType:  parsed.ErrorType,
		RawBody:    body,
	}
}

func setFloat(form url.Values, key string, value float64) {
	if value > 0 {
		form.Set(key, strconv.FormatFloat(value, 'f', 2, 64))
	}
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func checksum(apiKey, requestToken, apiSecret string) string {
	sum := sha256.Sum256([]byte(apiKey + requestToken + apiSecret))
	return fmt.Sprintf("%x", sum)
}
