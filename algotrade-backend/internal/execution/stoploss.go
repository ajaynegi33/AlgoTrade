package execution

import (
	"fmt"
	"math"
)

// =========================================================================================
// STOP LOSS & BREATHING BUFFER CONFIGURATION CHEAT-SHEET
// =========================================================================================
// Edit these constants to tune structural stop placement, breathing room, and slippage guards:
const (
	// StopMinTickMultiplier defines the absolute hard floor breathing room in exchange ticks.
	// 4 ticks * ₹0.05 = ₹0.20 minimum cushion even on low-volatility tight candles.
	StopMinTickMultiplier = 4.0

	// StopMinRiskPctCap defines the floor stop distance as % of LTP (0.35% LTP floor).
	StopMinRiskPctCap = 0.0035

	// StopMaxRiskPctCap defines the maximum allowed stop distance as % of LTP (0.80% LTP cap).
	// Prevents DeltaMin from ballooning on high-priced stocks.
	StopMaxRiskPctCap = 0.0080

	// StopMaxChaseMultiplier is the No-Chase Rule threshold. If the structural stop is more than
	// 1.25x DeltaMax away from the entry price, the candle is overextended and rejected.
	StopMaxChaseMultiplier = 1.25

	// StopBaseBufferLTPRatio calibrates dynamic breathing room across NSE price tiers:
	// - Stock < ₹250 (e.g. ₹190): 0.0075 * ₹190 = ~₹1.42 to ₹1.95 buffer
	// - Stock > ₹250 (e.g. ₹720): 0.0075 * ₹720 = ~₹5.40 buffer
	StopBaseBufferLTPRatio = 0.0075

	// StopLimitSlippageRatio defines the protective limit band placed below the trigger price
	// for NSE Stop-Loss Limit (SL-L) sell orders (0.40% or 4 ticks).
	StopLimitSlippageRatio = 0.0040
)

// =========================================================================================
// DOMAIN MODELS
// =========================================================================================

// StopAndBufferSpec holds the calibrated structural stop, ATR breathing buffer, and 1R distance
type StopAndBufferSpec struct {
	DeltaMin     float64 `json:"delta_min"`
	DeltaMax     float64 `json:"delta_max"`
	BaseBuffer   float64 `json:"base_buffer"`   // Dynamic breathing buffer (~₹1.50-₹2.00 for <₹250, ~₹5-₹6 for >₹250)
	InitialStop  float64 `json:"initial_stop"`  // Initial SL trigger price (placed below signal candle)
	OneR         float64 `json:"one_r"`         // Exact risk per share = EntryPrice - InitialStop
	SignalBarLow float64 `json:"signal_bar_low"`
}

// =========================================================================================
// CORE STOP LOSS CALCULATIONS
// =========================================================================================

// ComputeStopAndBufferSpec calculates non-inverting stop bounds (DeltaMin < DeltaMax),
// places a structural stop below the signal candle low, and decouples the dynamic breathing
// buffer (BaseBuffer) from tight intra-day entry candles.
//
// Formula & Step-by-Step Logic:
//  1. Safe ATR floor: math.Max(atr14, entryPrice * 0.0025)
//  2. Non-inverting DeltaMin: between 0.35% and 0.80% LTP, floor at 4 ticks.
//  3. Adaptive DeltaMax: between 1.15x ATR and 1.50% LTP.
//  4. Base breathing buffer: 0.75x ATR bounded within [DeltaMin, DeltaMax].
//  5. Signal candle low: raw stop = signalBarLow - 2 ticks.
//  6. No-Chase Rule: if raw stop distance > 1.25 * DeltaMax, the setup is rejected.
func ComputeStopAndBufferSpec( entryPrice float64, signalBarLow float64, atr14 float64, tickSize float64 ) (StopAndBufferSpec, error) {
	if entryPrice <= 0 {
		return StopAndBufferSpec{}, fmt.Errorf("invalid entry price: %.2f", entryPrice)
	}
	if tickSize <= 0 {
		tickSize = 0.05
	}

	// Floor ATR at 0.25% LTP to guard against zero or uninitialized historical bar data
	atrSafe := math.Max(atr14, entryPrice*0.0025)

	// Step 1: Strictly non-inverting bounds:
	// DeltaMin is capped at 0.80% LTP while DeltaMax adapts to ATR expansion
	deltaMin := math.Max(
		StopMinTickMultiplier*tickSize,
		math.Max(StopMinRiskPctCap*entryPrice, math.Min(0.35*atrSafe, StopMaxRiskPctCap*entryPrice)),
	)
	deltaMax := math.Max(
		deltaMin+StopMinTickMultiplier*tickSize,
		math.Min(1.15*atrSafe, math.Max(0.0150*entryPrice, 0.85*atrSafe)),
	)

	// Step 2: Base breathing buffer calibrated across NSE price tiers:
	rawBuffer := math.Max(0.75*atrSafe, StopBaseBufferLTPRatio*entryPrice)
	baseBuffer := RoundToTick(math.Max(deltaMin, math.Min(deltaMax, rawBuffer)), tickSize)

	// Step 3: Structural Stop placement below the signal candle low:
	rawStop := signalBarLow - 2.0*tickSize
	if rawStop <= 0 || rawStop >= entryPrice {
		rawStop = entryPrice - baseBuffer
	}
	rawDist := entryPrice - rawStop

	// Step 4: No-Chase Rule — if entry candle is overextended, reject the setup
	if rawDist > StopMaxChaseMultiplier*deltaMax {
		return StopAndBufferSpec{}, fmt.Errorf(
			"setup overextended: structural stop distance ₹%.2f exceeds %.2fx max allowed ₹%.2f",
			rawDist, StopMaxChaseMultiplier, deltaMax,
		)
	}

	// Step 5: Clamp stop distance within [DeltaMin, DeltaMax] and round to exchange ticks
	clampedDist := math.Max(deltaMin, math.Min(deltaMax, rawDist))
	oneR := RoundUpToTick(clampedDist, tickSize)
	initialStop := RoundDownToTick(entryPrice-oneR, tickSize)
	if initialStop <= 0 || initialStop >= entryPrice-tickSize {
		return StopAndBufferSpec{}, fmt.Errorf("invalid initial stop ₹%.2f for entry ₹%.2f", initialStop, entryPrice)
	}

	return StopAndBufferSpec{
		DeltaMin:     deltaMin,
		DeltaMax:     deltaMax,
		BaseBuffer:   baseBuffer,
		InitialStop:  initialStop,
		OneR:         entryPrice - initialStop,
		SignalBarLow: signalBarLow,
	}, nil
}

// BreakevenTriggerPrice computes the exact SL trigger price (rounded up to TickSize)
// on remaining runner shares so that even if the runner stops out at Breakeven,
// all round-trip statutory taxes & brokerage on the entire trade + a 2-tick/0.15% cushion are 100% covered.
func BreakevenTriggerPrice(entryPrice float64, initialQty, runnerQty int, tickSize float64) float64 {
	if tickSize <= 0 {
		tickSize = 0.05
	}
	if initialQty <= 0 {
		initialQty = 1
	}
	if runnerQty <= 0 {
		runnerQty = 1
	}

	// Calculate round-trip taxes on the original entry quantity
	taxes := CalculateRoundTripCharges(entryPrice, entryPrice, initialQty)
	perShareTax := taxes.TotalCharges / float64(runnerQty)

	// Cushion = max(perShareTax + 2 ticks, 0.15% of entry price)
	minCushion := math.Max(perShareTax+2.0*tickSize, 0.0015*entryPrice)
	return RoundUpToTick(entryPrice+minCushion, tickSize)
}

// ComputeSellSLLimitPrice calculates the protective limit price below triggerPrice
// for NSE Stop-Loss Limit (SL-L) orders. Placing the limit slightly below the trigger
// prevents resting orders from being gapped through in fast markets.
func ComputeSellSLLimitPrice(triggerPrice, tickSize float64) float64 {
	if tickSize <= 0 {
		tickSize = 0.05
	}
	slippageBand := math.Max(StopMinTickMultiplier*tickSize, StopLimitSlippageRatio*triggerPrice)
	return RoundDownToTick(math.Max(tickSize, triggerPrice-slippageBand), tickSize)
}

// computeSellSLLimitPrice is a package-private alias for ComputeSellSLLimitPrice
// ensuring backward-compatibility across all internal/execution callers.
func computeSellSLLimitPrice(triggerPrice, tickSize float64) float64 {
	return ComputeSellSLLimitPrice(triggerPrice, tickSize)
}
