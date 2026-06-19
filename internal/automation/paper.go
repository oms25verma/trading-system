package automation

import (
	"sort"
	"strings"
	"time"
)

const (
	PaperRunStatusReady     = "READY"
	PaperRunStatusBlocked   = "BLOCKED"
	PaperRunStatusViolation = "VIOLATION"
)

func RunPaper(store CandleStore, req PaperRunRequest) (*PaperRunResult, error) {
	req.BacktestRequest.Strategy = strings.ToLower(strings.TrimSpace(req.BacktestRequest.Strategy))
	result := &PaperRunResult{
		Request:     req,
		GeneratedAt: time.Now().UTC(),
	}
	preflight := preflightGuardrailViolations(req)
	if len(preflight) > 0 {
		result.Status = PaperRunStatusBlocked
		result.Reason = "guardrails blocked the paper run before strategy evaluation"
		result.Violations = preflight
		result.ID = paperRunID(result)
		return result, nil
	}

	backtest, err := NewBacktester(store).Run(req.BacktestRequest)
	if err != nil {
		return nil, err
	}
	result.Backtest = backtest
	result.Violations = append(result.Violations, resultGuardrailViolations(req.Guardrails, backtest.Trades)...)
	if len(result.Violations) > 0 {
		result.Status = PaperRunStatusViolation
		result.Reason = "strategy result breached one or more paper guardrails"
	} else {
		result.Status = PaperRunStatusReady
		result.Reason = "paper strategy result stayed within configured guardrails"
	}
	result.ID = paperRunID(result)
	return result, nil
}

func preflightGuardrailViolations(req PaperRunRequest) []GuardrailViolation {
	var out []GuardrailViolation
	if req.Guardrails.KillSwitch && !req.Guardrails.ManualOverride {
		out = append(out, GuardrailViolation{
			Code:    "kill_switch_active",
			Message: "kill switch is active; paper strategy is blocked until manual override is enabled",
		})
	}
	if req.Guardrails.MandatoryProtection && (req.StopLoss <= 0 || req.Target <= 0) {
		out = append(out, GuardrailViolation{
			Code:    "missing_protection",
			Message: "mandatory protection requires both stop_loss_points and target_points",
		})
	}
	if req.Guardrails.NoEntryAfter != "" {
		if _, err := parseClock(req.Guardrails.NoEntryAfter); err != nil {
			out = append(out, GuardrailViolation{
				Code:    "invalid_no_entry_after",
				Message: err.Error(),
			})
		}
	}
	if req.Guardrails.MaxTradesPerDay < 0 {
		out = append(out, GuardrailViolation{
			Code:    "invalid_max_trades_per_day",
			Message: "max_trades_per_day cannot be negative",
		})
	}
	if req.Guardrails.MaxDailyLoss < 0 {
		out = append(out, GuardrailViolation{
			Code:    "invalid_max_daily_loss",
			Message: "max_daily_loss cannot be negative",
		})
	}
	return out
}

func resultGuardrailViolations(guardrails Guardrails, trades []BacktestTrade) []GuardrailViolation {
	var out []GuardrailViolation
	if len(trades) == 0 {
		return out
	}
	byDayCount := map[string]int{}
	byDayPnL := map[string]float64{}
	noEntryAfter := -1
	if guardrails.NoEntryAfter != "" {
		if parsed, err := parseClock(guardrails.NoEntryAfter); err == nil {
			noEntryAfter = parsed
		}
	}
	for _, trade := range trades {
		day := istDay(trade.EntryTime)
		byDayCount[day]++
		byDayPnL[day] += trade.PnL
		if noEntryAfter >= 0 && clockMinutes(trade.EntryTime) > noEntryAfter {
			out = append(out, GuardrailViolation{
				Code:    "entry_after_cutoff",
				Message: "strategy entered after the configured no-entry cutoff",
				Day:     day,
				Value:   float64(clockMinutes(trade.EntryTime)),
				Limit:   float64(noEntryAfter),
			})
		}
	}
	days := make([]string, 0, len(byDayCount))
	for day := range byDayCount {
		days = append(days, day)
	}
	sort.Strings(days)
	for _, day := range days {
		count := byDayCount[day]
		if guardrails.MaxTradesPerDay > 0 && count > guardrails.MaxTradesPerDay {
			out = append(out, GuardrailViolation{
				Code:    "max_trades_per_day",
				Message: "strategy exceeded the max trades per day guardrail",
				Day:     day,
				Value:   float64(count),
				Limit:   float64(guardrails.MaxTradesPerDay),
			})
		}
		pnl := byDayPnL[day]
		if guardrails.MaxDailyLoss > 0 && pnl < -guardrails.MaxDailyLoss {
			out = append(out, GuardrailViolation{
				Code:    "max_daily_loss",
				Message: "strategy exceeded the max daily loss guardrail",
				Day:     day,
				Value:   pnl,
				Limit:   -guardrails.MaxDailyLoss,
			})
		}
	}
	return out
}

func istDay(value time.Time) string {
	location := time.FixedZone("IST", 5*60*60+30*60)
	return value.In(location).Format("2006-01-02")
}

func paperRunID(result *PaperRunResult) string {
	if result == nil {
		return "UNKNOWN"
	}
	if result.GeneratedAt.IsZero() {
		result.GeneratedAt = time.Now().UTC()
	}
	parts := []string{
		"paper",
		result.Request.Strategy,
		result.Request.Exchange,
		result.Request.TradingSymbol,
		result.Request.Interval,
		result.GeneratedAt.Format("20060102T150405000"),
	}
	return safePart(strings.ToLower(strings.Join(parts, "-")))
}

func summarizePaperRun(result *PaperRunResult) PaperRunSummary {
	if result == nil {
		return PaperRunSummary{}
	}
	summary := PaperRunSummary{
		ID:          result.ID,
		Status:      result.Status,
		Strategy:    result.Request.Strategy,
		Exchange:    result.Request.Exchange,
		Symbol:      result.Request.TradingSymbol,
		Interval:    result.Request.Interval,
		Violations:  len(result.Violations),
		GeneratedAt: result.GeneratedAt,
	}
	if result.Backtest != nil {
		summary.Trades = len(result.Backtest.Trades)
		summary.TotalPnL = result.Backtest.TotalPnL
	}
	return summary
}
