package trading

import "github.com/ajaynegi33/AlgoTrade/internal/market"

// Canonical IST location for execution engine
var ist = market.ISTLocation

// Standardized Structural Setup Patterns
const (
	PatternVWAPPullbackReclaim = "VWAP_PULLBACK_RECLAIM"
	PatternORB15MBreakout      = "ORB_15M_BREAKOUT"
)

// Trailing Stop Loss Gears
const (
	GearUnfilled     = 0 // Entry order pending fill
	GearBreakeven    = 1 // Stop ratcheted to Breakeven (+0.65R / +1.0R reached)
	GearDynamicTrail = 2 // Dynamic trailing behind 9 EMA / Swing Low (+1.5R reached)
	GearProfitLock   = 3 // Profit locked at +1.5R minimum (+2.5R reached)
	GearShadowExit   = 4 // Emergency Market Flatten (Adverse tape / Circuit / Mod 25 cap)
)

// Standardized Gear Labels for Frontend & Telemetry
const (
	GearLabelUnfilled     = "GEAR_0_UNFILLED"
	GearLabelBreakeven    = "GEAR_1_BREAKEVEN"
	GearLabelDynamicTrail = "GEAR_2_DYNAMIC_TRAIL"
	GearLabelProfitLock   = "GEAR_3_PROFIT_LOCK"
	GearLabelShadowExit   = "GEAR_4_SHADOW_EXIT"
)

// Regulatory Exchange Constraints
const (
	MaxOrderModifications  = 25 // NSE maximum permissible modifications per order
	MaxExchangeOrderMods   = MaxOrderModifications
	MaxSafetyModifications = 24 // Stop ratcheting caps at 24 to reserve mod #25 for market emergency exit
)

// Standardized Exit Reasons
const (
	ExitReasonInitialSL      = "INITIAL_SL_HIT"
	ExitReasonBreakevenSL    = "BREAKEVEN_SL_HIT"
	ExitReasonDynamicTrailSL = "TRAILING_STOP_HIT"
	ExitReasonProfitLockSL   = "PROFIT_LOCK_HIT"
	ExitReasonEmergencyTape  = "EMERGENCY_MACRO_SHOCK"
	ExitReasonEndOfDaySquare = "SESSION_EOD_SQUARE_OFF"
	ExitReasonEntryTimeout   = "ENTRY_TIMEOUT_CANCELLED"
	ExitReasonShadowStopHit  = "SHADOW_STOP_HIT"
	ExitReasonBrokerReconcile = "BROKER_RECONCILED"
)

