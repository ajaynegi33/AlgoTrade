package trading

import "github.com/ajaynegi33/AlgoTrade/internal/market"

// Canonical IST location for execution engine
var ist = market.ISTLocation

// Standardized Structural Setup Patterns
const (
	PatternVWAPPullbackReclaim    = "VWAP_PULLBACK_RECLAIM"
	PatternORBRetestReclaim       = "ORB_RETEST_RECLAIM"
	PatternFailedBreakdownReclaim = "FAILED_BREAKDOWN_SPRING"
)

// Trailing Stop Loss Gears
const (
	GearDynamicTrail = 2 // Dynamic trailing behind 9 EMA / Swing Low (+1.5R reached)
	GearProfitLock   = 3 // Profit locked at +1.5R minimum (+2.5R reached)
	GearShadowExit   = 4 // Emergency Market Flatten (Adverse tape / Circuit / Mod 25 cap)
)

// Regulatory Exchange Constraints
const (
	MaxExchangeOrderMods   = 25 // NSE maximum permissible modifications per order
	MaxSafetyModifications = 24 // Stop ratcheting caps at 24 to reserve mod #25 for market emergency exit
)

// Active Exit Reasons
const (
	ExitReasonShadowStopHit = "SHADOW_STOP_HIT"
)

