package automation

import "time"

type HistoricalRequest struct {
	Exchange        string    `json:"exchange"`
	TradingSymbol   string    `json:"tradingsymbol"`
	InstrumentToken int64     `json:"instrument_token"`
	Interval        string    `json:"interval"`
	From            time.Time `json:"from"`
	To              time.Time `json:"to"`
	Continuous      bool      `json:"continuous,omitempty"`
	IncludeOI       bool      `json:"include_oi,omitempty"`
}

type Candle struct {
	Time   time.Time `json:"time"`
	Open   float64   `json:"open"`
	High   float64   `json:"high"`
	Low    float64   `json:"low"`
	Close  float64   `json:"close"`
	Volume int64     `json:"volume"`
	OI     int64     `json:"oi,omitempty"`
}

type HistoricalSyncResult struct {
	Exchange        string    `json:"exchange"`
	TradingSymbol   string    `json:"tradingsymbol"`
	InstrumentToken int64     `json:"instrument_token"`
	Interval        string    `json:"interval"`
	From            time.Time `json:"from"`
	To              time.Time `json:"to"`
	CandlesFetched  int       `json:"candles_fetched"`
	CandlesStored   int       `json:"candles_stored"`
	Path            string    `json:"path"`
	Paths           []string  `json:"paths,omitempty"`
	SyncedAt        time.Time `json:"synced_at"`
}

type BacktestRequest struct {
	Exchange      string    `json:"exchange"`
	TradingSymbol string    `json:"tradingsymbol"`
	Interval      string    `json:"interval"`
	From          time.Time `json:"from"`
	To            time.Time `json:"to"`
	Strategy      string    `json:"strategy"`
	Quantity      int       `json:"quantity"`
	Multiplier    float64   `json:"multiplier"`
	StopLoss      float64   `json:"stop_loss_points"`
	Target        float64   `json:"target_points"`
	EntryBuffer   float64   `json:"entry_buffer_points,omitempty"`
	Slippage      float64   `json:"slippage_points,omitempty"`
	Brokerage     float64   `json:"brokerage_per_trade,omitempty"`
	RangeStart    string    `json:"range_start,omitempty"`
	RangeEnd      string    `json:"range_end,omitempty"`
	ExitTime      string    `json:"exit_time,omitempty"`
}

type BacktestTrade struct {
	EntryTime time.Time `json:"entry_time"`
	ExitTime  time.Time `json:"exit_time"`
	Side      string    `json:"side"`
	Entry     float64   `json:"entry"`
	Exit      float64   `json:"exit"`
	Quantity  int       `json:"quantity"`
	GrossPnL  float64   `json:"gross_pnl"`
	Costs     float64   `json:"costs"`
	PnL       float64   `json:"pnl"`
	Reason    string    `json:"reason"`
}

type EquityPoint struct {
	Time     time.Time `json:"time"`
	Equity   float64   `json:"equity"`
	Drawdown float64   `json:"drawdown"`
}

type BacktestResult struct {
	ID          string          `json:"id"`
	Strategy    string          `json:"strategy"`
	Exchange    string          `json:"exchange"`
	Symbol      string          `json:"tradingsymbol"`
	Interval    string          `json:"interval"`
	Trades      []BacktestTrade `json:"trades"`
	TotalPnL    float64         `json:"total_pnl"`
	GrossPnL    float64         `json:"gross_pnl"`
	TotalCosts  float64         `json:"total_costs"`
	MaxDrawdown float64         `json:"max_drawdown"`
	WinRate     float64         `json:"win_rate"`
	Expectancy  float64         `json:"expectancy"`
	AvgWin      float64         `json:"avg_win"`
	AvgLoss     float64         `json:"avg_loss"`
	EquityCurve []EquityPoint   `json:"equity_curve,omitempty"`
	CandlesUsed int             `json:"candles_used"`
	GeneratedAt time.Time       `json:"generated_at"`
}

type BacktestSummary struct {
	ID          string    `json:"id"`
	Strategy    string    `json:"strategy"`
	Exchange    string    `json:"exchange"`
	Symbol      string    `json:"tradingsymbol"`
	Interval    string    `json:"interval"`
	Trades      int       `json:"trades"`
	TotalPnL    float64   `json:"total_pnl"`
	GrossPnL    float64   `json:"gross_pnl"`
	TotalCosts  float64   `json:"total_costs"`
	MaxDrawdown float64   `json:"max_drawdown"`
	WinRate     float64   `json:"win_rate"`
	Expectancy  float64   `json:"expectancy"`
	GeneratedAt time.Time `json:"generated_at"`
}

type Guardrails struct {
	MandatoryProtection bool    `json:"mandatory_protection"`
	MaxTradesPerDay     int     `json:"max_trades_per_day,omitempty"`
	MaxDailyLoss        float64 `json:"max_daily_loss,omitempty"`
	NoEntryAfter        string  `json:"no_entry_after,omitempty"`
	KillSwitch          bool    `json:"kill_switch"`
	ManualOverride      bool    `json:"manual_override"`
}

type PaperRunRequest struct {
	BacktestRequest
	Guardrails Guardrails `json:"guardrails"`
}

type GuardrailViolation struct {
	Code    string  `json:"code"`
	Message string  `json:"message"`
	Day     string  `json:"day,omitempty"`
	Value   float64 `json:"value,omitempty"`
	Limit   float64 `json:"limit,omitempty"`
}

type PaperRunResult struct {
	ID          string               `json:"id"`
	Status      string               `json:"status"`
	Reason      string               `json:"reason,omitempty"`
	Request     PaperRunRequest      `json:"request"`
	Backtest    *BacktestResult      `json:"backtest,omitempty"`
	Violations  []GuardrailViolation `json:"violations,omitempty"`
	GeneratedAt time.Time            `json:"generated_at"`
}

type PaperRunSummary struct {
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	Strategy    string    `json:"strategy"`
	Exchange    string    `json:"exchange"`
	Symbol      string    `json:"tradingsymbol"`
	Interval    string    `json:"interval"`
	Trades      int       `json:"trades"`
	TotalPnL    float64   `json:"total_pnl"`
	Violations  int       `json:"violations"`
	GeneratedAt time.Time `json:"generated_at"`
}
