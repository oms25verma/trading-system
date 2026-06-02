package trading

import "strings"

const (
	LocalSystemOrderTag = "TSLOCAL"

	CreationSourceLocalSystem = "LOCAL_SYSTEM"
	CreationSourceKiteApp     = "KITE_APP"
	CreationSourceUnknown     = "UNKNOWN"

	ManagementStatusManaged          = "MANAGED"
	ManagementStatusUnmanaged        = "UNMANAGED"
	ManagementStatusPartiallyManaged = "PARTIALLY_MANAGED"

	WarningOppositeExposureAcrossProducts = "OPPOSITE_EXPOSURE_ACROSS_PRODUCTS"
	WarningPartialExternalExit            = "PARTIAL_EXTERNAL_EXIT"
	WarningPositionQuantityMismatch       = "POSITION_QUANTITY_MISMATCH"
	WarningPositionMissingFromBroker      = "POSITION_MISSING_FROM_BROKER"
	WarningProductConversionDetected      = "PRODUCT_CONVERSION_DETECTED"

	DefaultSLLimitOffset          = 0.05
	DefaultCommoditySLLimitOffset = 10.0

	TradeStatusOpen   = "OPEN"
	TradeStatusClosed = "CLOSED"

	ExitReasonManual         = "MANUAL"
	ExitReasonManualExternal = "MANUAL_EXTERNAL"
	ExitReasonStopLoss       = "STOP_LOSS"
	ExitReasonTarget         = "TARGET"
	ExitReasonBothCompleted  = "BOTH_COMPLETED"

	OrderStatusOpen      = "OPEN"
	OrderStatusComplete  = "COMPLETE"
	OrderStatusCancelled = "CANCELLED"
	OrderStatusRejected  = "REJECTED"
)

func defaultSLLimitOffset(exchange string) float64 {
	if strings.EqualFold(exchange, "MCX") {
		return DefaultCommoditySLLimitOffset
	}
	return DefaultSLLimitOffset
}
