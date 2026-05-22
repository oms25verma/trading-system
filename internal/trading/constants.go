package trading

import "strings"

const (
	DefaultSLLimitOffset          = 0.05
	DefaultCommoditySLLimitOffset = 10.0
)

func defaultSLLimitOffset(exchange string) float64 {
	if strings.EqualFold(exchange, "MCX") {
		return DefaultCommoditySLLimitOffset
	}
	return DefaultSLLimitOffset
}
