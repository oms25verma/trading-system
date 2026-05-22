package kite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const baseURL = "https://api.kite.trade"

type Client struct {
	apiKey      string
	apiSecret   string
	accessToken string
	httpClient  *http.Client
}

func NewClient(apiKey, apiSecret, accessToken string) *Client {
	return &Client{
		apiKey:      apiKey,
		apiSecret:   apiSecret,
		accessToken: accessToken,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
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
}

type OrderResponse struct {
	OrderID string `json:"order_id"`
}

type OrderStatusResponse struct {
	Status string `json:"status"`
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
	var out struct {
		Data []OrderStatusResponse `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/orders/"+orderID, nil, &out); err != nil {
		return "", err
	}
	if len(out.Data) == 0 {
		return "", fmt.Errorf("order history not found for %s", orderID)
	}
	return out.Data[len(out.Data)-1].Status, nil
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
	return c.do(ctx, method, path, body, out)
}

func (c *Client) doPublicForm(ctx context.Context, method, path string, form url.Values, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("X-Kite-Version", "3")
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("kite %s %s failed: status=%d body=%s", method, path, resp.StatusCode, bytes.TrimSpace(payload))
	}
	if out == nil || len(payload) == 0 {
		return nil
	}
	return json.Unmarshal(payload, out)
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, out any) error {
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

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("kite %s %s failed: status=%d body=%s", method, path, resp.StatusCode, bytes.TrimSpace(payload))
	}
	if out == nil || len(payload) == 0 {
		return nil
	}
	return json.Unmarshal(payload, out)
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
