package kite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
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
	"trading-system/internal/trading"
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
	Exchange      string  `json:"exchange"`
	TradingSymbol string  `json:"tradingsymbol"`
	Product       string  `json:"product"`
	Quantity      int     `json:"quantity"`
	AveragePrice  float64 `json:"average_price"`
	LastPrice     float64 `json:"last_price"`
	PnL           float64 `json:"pnl"`
}

type HistoricalRequest struct {
	InstrumentToken int64
	Interval        string
	From            time.Time
	To              time.Time
	Continuous      bool
	IncludeOI       bool
}

type HistoricalCandle struct {
	Time   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume int64
	OI     int64
}

type Instrument struct {
	InstrumentToken uint32
	ExchangeToken   string
	TradingSymbol   string
	Name            string
	LastPrice       float64
	Expiry          string
	Strike          float64
	TickSize        float64
	LotSize         int
	InstrumentType  string
	Segment         string
	Exchange        string
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

func (c *Client) LTPBatch(ctx context.Context, instruments []trading.InstrumentRef) (map[string]trading.MarketQuote, error) {
	query := url.Values{}
	requested := make(map[string]trading.InstrumentRef, len(instruments))
	for _, instrument := range instruments {
		exchange := strings.ToUpper(strings.TrimSpace(instrument.Exchange))
		symbol := strings.ToUpper(strings.TrimSpace(instrument.TradingSymbol))
		if exchange == "" || symbol == "" {
			continue
		}
		key := exchange + ":" + symbol
		if _, ok := requested[key]; ok {
			continue
		}
		requested[key] = trading.InstrumentRef{Exchange: exchange, TradingSymbol: symbol}
		query.Add("i", key)
	}
	if len(requested) == 0 {
		return map[string]trading.MarketQuote{}, nil
	}
	var out struct {
		Data map[string]struct {
			LastPrice float64 `json:"last_price"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/quote/ltp?"+query.Encode(), nil, &out); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	quotes := make(map[string]trading.MarketQuote, len(out.Data))
	for key, quote := range out.Data {
		instrument := requested[key]
		quotes[key] = trading.MarketQuote{
			Exchange:      instrument.Exchange,
			TradingSymbol: instrument.TradingSymbol,
			LastPrice:     quote.LastPrice,
			SyncedAt:      now,
		}
	}
	return quotes, nil
}

func (c *Client) Instruments(ctx context.Context, exchange string) ([]Instrument, error) {
	path := "/instruments"
	if strings.TrimSpace(exchange) != "" {
		path += "/" + url.PathEscape(strings.ToUpper(strings.TrimSpace(exchange)))
	}
	payload, err := c.doRaw(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	return parseInstrumentsCSV(payload)
}

func (c *Client) HistoricalCandles(ctx context.Context, req HistoricalRequest) ([]HistoricalCandle, error) {
	if req.InstrumentToken <= 0 {
		return nil, errors.New("missing instrument token")
	}
	interval := strings.ToLower(strings.TrimSpace(req.Interval))
	if interval == "" {
		return nil, errors.New("missing historical interval")
	}
	query := url.Values{}
	query.Set("from", req.From.Format("2006-01-02 15:04:05"))
	query.Set("to", req.To.Format("2006-01-02 15:04:05"))
	if req.Continuous {
		query.Set("continuous", "1")
	}
	if req.IncludeOI {
		query.Set("oi", "1")
	}
	path := "/instruments/historical/" + strconv.FormatInt(req.InstrumentToken, 10) + "/" + url.PathEscape(interval) + "?" + query.Encode()
	var out struct {
		Data struct {
			Candles [][]any `json:"candles"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	candles := make([]HistoricalCandle, 0, len(out.Data.Candles))
	for _, raw := range out.Data.Candles {
		candle, err := parseHistoricalCandle(raw)
		if err != nil {
			return nil, err
		}
		candles = append(candles, candle)
	}
	return candles, nil
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

func (c *Client) doRaw(ctx context.Context, method, path string, body io.Reader) ([]byte, error) {
	if c.apiKey == "" || c.accessToken == "" {
		return nil, errors.New("missing KITE_API_KEY or KITE_ACCESS_TOKEN")
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Kite-Version", "3")
	req.Header.Set("Authorization", "token "+c.apiKey+":"+c.accessToken)

	startedAt := time.Now()
	c.logBrokerRequest(ctx, method, path, nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logBrokerError(ctx, method, path, time.Since(startedAt), err)
		return nil, err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logBrokerError(ctx, method, path, time.Since(startedAt), err)
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.logBrokerResponse(ctx, method, path, resp.StatusCode, time.Since(startedAt), payload, true)
		return nil, kiteAPIError(method, path, resp.StatusCode, payload)
	}
	c.logBrokerResponse(ctx, method, path, resp.StatusCode, time.Since(startedAt), payload, false)
	return payload, nil
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
		if isExpectedBrokerRejection(statusCode, payload) {
			c.logger.InfoContext(ctx, "broker_request_rejected", args...)
			return
		}
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

func isExpectedBrokerRejection(statusCode int, payload []byte) bool {
	if statusCode != http.StatusBadRequest {
		return false
	}
	var parsed struct {
		ErrorType string `json:"error_type"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return false
	}
	return strings.EqualFold(parsed.ErrorType, "InputException")
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

func parseInstrumentsCSV(payload []byte) ([]Instrument, error) {
	reader := csv.NewReader(bytes.NewReader(payload))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) <= 1 {
		return nil, nil
	}
	out := make([]Instrument, 0, len(records)-1)
	for _, record := range records[1:] {
		if len(record) < 12 {
			continue
		}
		token, _ := strconv.ParseUint(record[0], 10, 32)
		lastPrice, _ := strconv.ParseFloat(emptyFloat(record[4]), 64)
		strike, _ := strconv.ParseFloat(emptyFloat(record[6]), 64)
		tickSize, _ := strconv.ParseFloat(emptyFloat(record[7]), 64)
		lotSize, _ := strconv.Atoi(emptyInt(record[8]))
		out = append(out, Instrument{
			InstrumentToken: uint32(token),
			ExchangeToken:   strings.TrimSpace(record[1]),
			TradingSymbol:   strings.ToUpper(strings.TrimSpace(record[2])),
			Name:            strings.ToUpper(strings.TrimSpace(record[3])),
			LastPrice:       lastPrice,
			Expiry:          strings.TrimSpace(record[5]),
			Strike:          strike,
			TickSize:        tickSize,
			LotSize:         lotSize,
			InstrumentType:  strings.ToUpper(strings.TrimSpace(record[9])),
			Segment:         strings.ToUpper(strings.TrimSpace(record[10])),
			Exchange:        strings.ToUpper(strings.TrimSpace(record[11])),
		})
	}
	return out, nil
}

func parseHistoricalCandle(raw []any) (HistoricalCandle, error) {
	if len(raw) < 6 {
		return HistoricalCandle{}, fmt.Errorf("historical candle has %d fields, expected at least 6", len(raw))
	}
	timestamp, ok := raw[0].(string)
	if !ok || strings.TrimSpace(timestamp) == "" {
		return HistoricalCandle{}, fmt.Errorf("historical candle timestamp is invalid")
	}
	at, err := parseKiteTime(timestamp)
	if err != nil {
		return HistoricalCandle{}, err
	}
	candle := HistoricalCandle{
		Time:   at.UTC(),
		Open:   numberAt(raw, 1),
		High:   numberAt(raw, 2),
		Low:    numberAt(raw, 3),
		Close:  numberAt(raw, 4),
		Volume: int64(numberAt(raw, 5)),
	}
	if len(raw) > 6 {
		candle.OI = int64(numberAt(raw, 6))
	}
	return candle, nil
}

func parseKiteTime(value string) (time.Time, error) {
	layouts := []string{
		"2006-01-02T15:04:05-0700",
		time.RFC3339,
		"2006-01-02 15:04:05",
	}
	var lastErr error
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, nil
		}
		lastErr = err
	}
	return time.Time{}, fmt.Errorf("parse kite time %q: %w", value, lastErr)
}

func numberAt(raw []any, index int) float64 {
	if index >= len(raw) {
		return 0
	}
	switch value := raw[index].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case json.Number:
		number, _ := value.Float64()
		return number
	default:
		return 0
	}
}

func emptyFloat(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "0"
	}
	return raw
}

func emptyInt(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "0"
	}
	return raw
}
