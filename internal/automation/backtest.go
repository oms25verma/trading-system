package automation

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type Backtester struct {
	store CandleStore
}

func NewBacktester(store CandleStore) *Backtester {
	return &Backtester{store: store}
}

func (b *Backtester) Run(req BacktestRequest) (*BacktestResult, error) {
	if b.store == nil {
		return nil, fmt.Errorf("historical store is not configured")
	}
	req.Exchange = strings.ToUpper(strings.TrimSpace(req.Exchange))
	req.TradingSymbol = strings.ToUpper(strings.TrimSpace(req.TradingSymbol))
	req.Interval = strings.ToLower(strings.TrimSpace(req.Interval))
	req.Strategy = strings.ToLower(strings.TrimSpace(req.Strategy))
	if req.Quantity <= 0 {
		req.Quantity = 1
	}
	if req.Multiplier <= 0 {
		req.Multiplier = 1
	}
	if req.Slippage < 0 {
		req.Slippage = 0
	}
	if req.Brokerage < 0 {
		req.Brokerage = 0
	}
	if req.RangeStart == "" {
		req.RangeStart = "09:15"
	}
	if req.RangeEnd == "" {
		req.RangeEnd = "09:30"
	}
	if req.ExitTime == "" {
		req.ExitTime = "15:20"
	}
	if err := validateStoreRequest(req.Exchange, req.TradingSymbol, req.Interval); err != nil {
		return nil, err
	}
	if req.From.IsZero() || req.To.IsZero() || !req.From.Before(req.To) {
		return nil, fmt.Errorf("valid from/to range is required")
	}
	candles, err := b.store.Load(req)
	if err != nil {
		return nil, err
	}
	result := &BacktestResult{
		Strategy:    req.Strategy,
		Exchange:    req.Exchange,
		Symbol:      req.TradingSymbol,
		Interval:    req.Interval,
		CandlesUsed: len(candles),
		GeneratedAt: time.Now().UTC(),
	}
	if req.Strategy == "" || req.Strategy == "none" {
		result.ID = backtestID(result)
		return result, nil
	}
	switch req.Strategy {
	case "opening_range_breakout", "orb":
		trades, err := runOpeningRangeBreakout(candles, req)
		if err != nil {
			return nil, err
		}
		applyBacktestStats(result, trades)
		result.ID = backtestID(result)
		return result, nil
	default:
		return nil, fmt.Errorf("strategy %q is not implemented yet", req.Strategy)
	}
}

func runOpeningRangeBreakout(candles []Candle, req BacktestRequest) ([]BacktestTrade, error) {
	rangeStart, err := parseClock(req.RangeStart)
	if err != nil {
		return nil, err
	}
	rangeEnd, err := parseClock(req.RangeEnd)
	if err != nil {
		return nil, err
	}
	exitClock, err := parseClock(req.ExitTime)
	if err != nil {
		return nil, err
	}
	byDay := candlesByISTDay(candles)
	days := make([]string, 0, len(byDay))
	for day := range byDay {
		days = append(days, day)
	}
	sort.Strings(days)

	var trades []BacktestTrade
	for _, day := range days {
		dayCandles := byDay[day]
		if len(dayCandles) == 0 {
			continue
		}
		rangeHigh := math.Inf(-1)
		rangeLow := math.Inf(1)
		for _, candle := range dayCandles {
			minute := clockMinutes(candle.Time)
			if minute < rangeStart || minute > rangeEnd {
				continue
			}
			rangeHigh = math.Max(rangeHigh, candle.High)
			rangeLow = math.Min(rangeLow, candle.Low)
		}
		if math.IsInf(rangeHigh, -1) || math.IsInf(rangeLow, 1) {
			continue
		}
		var open *BacktestTrade
		for _, candle := range dayCandles {
			minute := clockMinutes(candle.Time)
			if minute <= rangeEnd {
				continue
			}
			if minute >= exitClock {
				if open != nil {
					open.ExitTime = candle.Time
					open.Exit = applyExitSlippage(open.Side, candle.Close, req.Slippage)
					open.Reason = "CUTOFF"
					applyTradePnL(open, req)
					trades = append(trades, *open)
				}
				break
			}
			if open == nil {
				buyTrigger := rangeHigh + req.EntryBuffer
				sellTrigger := rangeLow - req.EntryBuffer
				if candle.High >= buyTrigger {
					open = &BacktestTrade{EntryTime: candle.Time, Side: "BUY", Entry: applyEntrySlippage("BUY", buyTrigger, req.Slippage), Quantity: req.Quantity}
				} else if candle.Low <= sellTrigger {
					open = &BacktestTrade{EntryTime: candle.Time, Side: "SELL", Entry: applyEntrySlippage("SELL", sellTrigger, req.Slippage), Quantity: req.Quantity}
				}
				continue
			}
			exit, reason, hit := exitForCandle(*open, candle, req)
			if hit {
				open.ExitTime = candle.Time
				open.Exit = exit
				open.Reason = reason
				applyTradePnL(open, req)
				trades = append(trades, *open)
				open = nil
				break
			}
		}
		if open != nil {
			last := dayCandles[len(dayCandles)-1]
			open.ExitTime = last.Time
			open.Exit = applyExitSlippage(open.Side, last.Close, req.Slippage)
			open.Reason = "EOD"
			applyTradePnL(open, req)
			trades = append(trades, *open)
		}
	}
	return trades, nil
}

func exitForCandle(trade BacktestTrade, candle Candle, req BacktestRequest) (float64, string, bool) {
	if trade.Side == "BUY" {
		stop := trade.Entry - req.StopLoss
		target := trade.Entry + req.Target
		if req.StopLoss > 0 && candle.Low <= stop {
			return applyExitSlippage(trade.Side, stop, req.Slippage), "STOP_LOSS", true
		}
		if req.Target > 0 && candle.High >= target {
			return applyExitSlippage(trade.Side, target, req.Slippage), "TARGET", true
		}
		return 0, "", false
	}
	stop := trade.Entry + req.StopLoss
	target := trade.Entry - req.Target
	if req.StopLoss > 0 && candle.High >= stop {
		return applyExitSlippage(trade.Side, stop, req.Slippage), "STOP_LOSS", true
	}
	if req.Target > 0 && candle.Low <= target {
		return applyExitSlippage(trade.Side, target, req.Slippage), "TARGET", true
	}
	return 0, "", false
}

func applyEntrySlippage(side string, price, slippage float64) float64 {
	if side == "SELL" {
		return price - slippage
	}
	return price + slippage
}

func applyExitSlippage(side string, price, slippage float64) float64 {
	if side == "SELL" {
		return price + slippage
	}
	return price - slippage
}

func applyTradePnL(trade *BacktestTrade, req BacktestRequest) {
	if trade == nil {
		return
	}
	trade.GrossPnL = tradeGrossPnL(*trade, req.Multiplier)
	trade.Costs = req.Brokerage
	trade.PnL = trade.GrossPnL - trade.Costs
}

func tradeGrossPnL(trade BacktestTrade, multiplier float64) float64 {
	if multiplier <= 0 {
		multiplier = 1
	}
	diff := trade.Exit - trade.Entry
	if trade.Side == "SELL" {
		diff = trade.Entry - trade.Exit
	}
	return diff * float64(trade.Quantity) * multiplier
}

func applyBacktestStats(result *BacktestResult, trades []BacktestTrade) {
	result.Trades = trades
	if len(trades) == 0 {
		return
	}
	wins := 0
	losses := 0
	winTotal := 0.0
	lossTotal := 0.0
	equity := 0.0
	peak := 0.0
	maxDrawdown := 0.0
	for _, trade := range trades {
		result.GrossPnL += trade.GrossPnL
		result.TotalCosts += trade.Costs
		result.TotalPnL += trade.PnL
		if trade.PnL > 0 {
			wins++
			winTotal += trade.PnL
		} else if trade.PnL < 0 {
			losses++
			lossTotal += trade.PnL
		}
		equity += trade.PnL
		if equity > peak {
			peak = equity
		}
		drawdown := peak - equity
		if drawdown > maxDrawdown {
			maxDrawdown = drawdown
		}
		result.EquityCurve = append(result.EquityCurve, EquityPoint{
			Time:     trade.ExitTime,
			Equity:   equity,
			Drawdown: drawdown,
		})
	}
	result.MaxDrawdown = maxDrawdown
	result.WinRate = float64(wins) / float64(len(trades)) * 100
	result.Expectancy = result.TotalPnL / float64(len(trades))
	if wins > 0 {
		result.AvgWin = winTotal / float64(wins)
	}
	if losses > 0 {
		result.AvgLoss = lossTotal / float64(losses)
	}
}

func candlesByISTDay(candles []Candle) map[string][]Candle {
	location := time.FixedZone("IST", 5*60*60+30*60)
	out := make(map[string][]Candle)
	for _, candle := range candles {
		day := candle.Time.In(location).Format("2006-01-02")
		out[day] = append(out[day], candle)
	}
	for day := range out {
		sort.Slice(out[day], func(i, j int) bool { return out[day][i].Time.Before(out[day][j].Time) })
	}
	return out
}

func parseClock(value string) (int, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid clock value %q, expected HH:MM", value)
	}
	hour, err := parseSmallInt(parts[0])
	if err != nil {
		return 0, err
	}
	minute, err := parseSmallInt(parts[1])
	if err != nil {
		return 0, err
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("invalid clock value %q", value)
	}
	return hour*60 + minute, nil
}

func parseSmallInt(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("invalid number %q", value)
	}
	out := 0
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, fmt.Errorf("invalid number %q", value)
		}
		out = out*10 + int(char-'0')
	}
	return out, nil
}

func clockMinutes(value time.Time) int {
	location := time.FixedZone("IST", 5*60*60+30*60)
	local := value.In(location)
	return local.Hour()*60 + local.Minute()
}

func backtestID(result *BacktestResult) string {
	if result.GeneratedAt.IsZero() {
		result.GeneratedAt = time.Now().UTC()
	}
	parts := []string{
		result.Strategy,
		result.Exchange,
		result.Symbol,
		result.Interval,
		result.GeneratedAt.Format("20060102T150405000"),
	}
	return safePart(strings.ToLower(strings.Join(parts, "-")))
}

func summarizeBacktest(result *BacktestResult) BacktestSummary {
	if result == nil {
		return BacktestSummary{}
	}
	return BacktestSummary{
		ID:          result.ID,
		Strategy:    result.Strategy,
		Exchange:    result.Exchange,
		Symbol:      result.Symbol,
		Interval:    result.Interval,
		Trades:      len(result.Trades),
		TotalPnL:    result.TotalPnL,
		GrossPnL:    result.GrossPnL,
		TotalCosts:  result.TotalCosts,
		MaxDrawdown: result.MaxDrawdown,
		WinRate:     result.WinRate,
		Expectancy:  result.Expectancy,
		GeneratedAt: result.GeneratedAt,
	}
}
