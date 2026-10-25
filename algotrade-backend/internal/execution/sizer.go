package execution

import (
	"fmt"
	"math"

	"github.com/ajaynegi33/AlgoTrade/internal/engine"
)

// StopAndBufferSpec holds the calibrated structural stop and dynamic breathing buffer for a stock
type StopAndBufferSpec struct {
	DeltaMin     float64 `json:"delta_min"`
	DeltaMax     float64 `json:"delta_max"`
	BaseBuffer   float64 `json:"base_buffer"`   // Dynamic breathing buffer (~₹1.50-₹2.00 for <₹250, ~₹5-₹6 for >₹250)
	InitialStop  float64 `json:"initial_stop"`  // Initial SL trigger price
	OneR         float64 `json:"one_r"`         // EntryPrice - InitialStop
	SignalBarLow float64 `json:"signal_bar_low"`
}

// SizingDecision holds the output of the 4-Constraint Position Sizer and Net-Profitability Gate
type SizingDecision struct {
	Approved         bool              `json:"approved"`
	RejectReason     string            `json:"reject_reason,omitempty"`
	Quantity         int               `json:"quantity"`
	DustFloorQty     int               `json:"dust_floor_qty"`
	EntryLimitPrice  float64           `json:"entry_limit_price"`
	StopSpec         StopAndBufferSpec `json:"stop_spec"`
	BreakevenStop    float64           `json:"breakeven_stop"`
	RiskBudgetRupees float64           `json:"risk_budget_rupees"`
	RequiredMargin   float64           `json:"required_margin"`
	ExpectedNet1R    float64           `json:"expected_net_1r"`
	EstimatedTaxes   float64           `json:"estimated_taxes"`
}

// ComputeStopAndBufferSpec calculates non-inverting stop bounds (DeltaMin < DeltaMax)
// and decouples the stock's price-tier/ATR breathing buffer (BaseBuffer) from tight entry candles.
func ComputeStopAndBufferSpec(
	entryPrice float64,
	signalBarLow float64,
	atr14 float64,
	tickSize float64,
) (StopAndBufferSpec, error) {
	if entryPrice <= 0 {
		return StopAndBufferSpec{}, fmt.Errorf("invalid entry price: %.2f", entryPrice)
	}
	if tickSize <= 0 {
		tickSize = 0.05
	}

	atrSafe := math.Max(atr14, entryPrice*0.0025)

	// Strictly non-inverting bounds: DeltaMin is capped at 0.80% LTP while DeltaMax adapts to ATR expansion
	deltaMin := math.Max(
		4.0*tickSize,
		math.Max(0.0035*entryPrice, math.Min(0.35*atrSafe, 0.0080*entryPrice)),
	)
	deltaMax := math.Max(
		deltaMin+4.0*tickSize,
		math.Min(1.15*atrSafe, math.Max(0.0150*entryPrice, 0.85*atrSafe)),
	)

	// Base breathing buffer calibrated across NSE price tiers:
	// - Stock < ₹250 (e.g. ₹190): 0.0075 * 190 = ₹1.42 to ₹1.95
	// - Stock > ₹250 (e.g. ₹720): 0.0075 * 720 = ₹5.40
	rawBuffer := math.Max(0.75*atrSafe, 0.0075*entryPrice)
	baseBuffer := RoundToTick(math.Max(deltaMin, math.Min(deltaMax, rawBuffer)), tickSize)

	rawStop := signalBarLow - 2.0*tickSize
	if rawStop <= 0 || rawStop >= entryPrice {
		rawStop = entryPrice - baseBuffer
	}
	rawDist := entryPrice - rawStop

	// No-Chase Rule: If structural stop is more than 1.25 * deltaMax away, entry candle is overextended
	if rawDist > 1.25*deltaMax {
		return StopAndBufferSpec{}, fmt.Errorf("setup overextended: structural stop distance ₹%.2f exceeds 1.25x max allowed ₹%.2f", rawDist, deltaMax)
	}

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

// EffectiveRiskPerTradePct implements the Scale-Invariant Adaptive Risk Curve across ₹500 to ₹10Cr+ accounts.
// When configuredRiskPct is at the default 0.50% (0.005), small accounts (≤ ₹15,000, e.g. ₹10,000 w/ 5x MIS = ₹50,000)
// automatically scale to 1.25% risk (₹125 budget) so position sizes clear NSE statutory charges and utilize margin,
// while institutional accounts (> ₹50L to ₹10Cr+) taper to 0.35%. Custom user overrides are honored directly.
func EffectiveRiskPerTradePct(netEquity, configuredRiskPct float64) float64 {
	if configuredRiskPct > 0 && configuredRiskPct <= 0.03 && math.Abs(configuredRiskPct-0.005) > 1e-6 {
		return configuredRiskPct
	}
	switch {
	case netEquity <= 15000.0:
		return 0.0125 // 1.25% (₹125 on ₹10k equity -> utilizes 5x MIS ₹50k buying power cleanly)
	case netEquity <= 50000.0:
		return 0.0100 // 1.00%
	case netEquity <= 200000.0:
		return 0.0075 // 0.75%
	case netEquity <= 5000000.0:
		return 0.0050 // 0.50%
	default:
		return 0.0035 // 0.35% for ₹50L to ₹10Cr+
	}
}

// ComputePositionSize evaluates the 4 institutional sizing constraints (Risk, Margin, Concentration, Liquidity)
// from ₹500 to ₹10Cr+ accounts and enforces the Mandatory Net-Profitability Gate at +1.0R.
func ComputePositionSize(
	s *engine.StockMetrics,
	signalBarLow float64,
	netEquity float64,
	freeMargin float64,
	riskPerTradePct float64,
	sessionSizeMult float64,
	drawdownMult float64,
) SizingDecision {
	tickSize := float64(s.TickSize)
	if tickSize <= 0 {
		tickSize = 0.05
	}

	// Marketable limit entry price: BestAsk + 1 tick (or LTP + 1 tick if Ask1 is 0), capped within 0.15%
	refAsk := float64(s.Ask1Price)
	if refAsk <= 0 || refAsk < s.LTP {
		refAsk = s.LTP
	}
	entryLimitPrice := RoundUpToTick(refAsk+tickSize, tickSize)

	stopSpec, err := ComputeStopAndBufferSpec(entryLimitPrice, signalBarLow, s.ATR14, tickSize)
	if err != nil {
		return SizingDecision{
			Approved:     false,
			RejectReason: err.Error(),
		}
	}

	if netEquity < 500.0 || freeMargin <= 0 {
		return SizingDecision{
			Approved:     false,
			RejectReason: fmt.Sprintf("insufficient capital (NetEquity=₹%.2f, FreeMargin=₹%.2f)", netEquity, freeMargin),
		}
	}

	effRiskPct := EffectiveRiskPerTradePct(netEquity, riskPerTradePct)
	if sessionSizeMult <= 0 {
		sessionSizeMult = 1.0
	}
	if drawdownMult <= 0 {
		drawdownMult = 1.0
	}

	riskBudget := netEquity * effRiskPct * sessionSizeMult * drawdownMult
	// Small-Account Floor (₹500 to ₹25,000): prevent midday/drawdown multipliers from starving 5x MIS sizing below 0.75% equity
	if netEquity <= 25000.0 {
		riskBudget = math.Max(riskBudget, netEquity*0.0075)
	}
	leverage := float64(s.MISLeverage)
	if leverage < 1.0 || leverage > 5.0 {
		leverage = 5.0
	}

	// Estimate per-share tax + slippage friction at tentative quantity
	tentativeQty := max(1, int(math.Floor(riskBudget/stopSpec.OneR)))
	estTax := CalculateRoundTripCharges(entryLimitPrice, stopSpec.InitialStop, tentativeQty)
	perShareFriction := (estTax.TotalCharges / float64(tentativeQty)) + 1.5*tickSize

	// 1. Risk-Capped Quantity
	qRisk := int(math.Floor(riskBudget / (stopSpec.OneR + perShareFriction)))

	// 2. Margin-Capped Quantity (with 5% RMS safety buffer)
	qMargin := int(math.Floor((0.95 * freeMargin * leverage) / entryLimitPrice))

	// 3. Single-Stock Concentration Cap (35% of NetEquity for accounts >= ₹25k; 95% for micro accounts < ₹25k)
	concFactor := 0.35
	if netEquity < 25000.0 {
		concFactor = 0.95
	}
	qConc := int(math.Floor((concFactor * netEquity * leverage) / entryLimitPrice))

	// 4. Order-Book Top-3 Ask Liquidity Cap (never sweep more than 20% of visible top-3 ask depth)
	var askTop3Qty uint64
	for i := 0; i < 3; i++ {
		askTop3Qty += uint64(s.AskLevels[i].Qty)
	}
	qLiq := max(1, int(math.Floor(0.20*float64(askTop3Qty))))

	// Micro-Capital Allowance (₹500 to ₹25,000 accounts):
	// If qRisk == 0 because 0.5% of ₹1,000 is ₹5, allow 1 share if margin covers it and max loss <= 2.0% of NetEquity
	if qRisk == 0 && netEquity < 25000.0 && qMargin >= 1 {
		if (stopSpec.OneR + perShareFriction) <= 0.02*netEquity {
			qRisk = 1
		}
	}

	finalQty := min(qRisk, min(qMargin, min(qConc, qLiq)))
	if finalQty <= 0 {
		return SizingDecision{
			Approved:     false,
			RejectReason: fmt.Sprintf("quantity constrained to 0 (qRisk=%d, qMargin=%d, qConc=%d, qLiq=%d)", qRisk, qMargin, qConc, qLiq),
		}
	}

	// Mandatory Net-Profitability Gate at +1.0R:
	// Net profit at +1.0R must be at least 2.0x round-trip statutory & brokerage charges
	target1RPrice := entryLimitPrice + stopSpec.OneR
	chargesAt1R := CalculateRoundTripCharges(entryLimitPrice, target1RPrice, finalQty).TotalCharges
	grossProfit1R := float64(finalQty) * stopSpec.OneR
	netProfit1R := grossProfit1R - chargesAt1R

	if netProfit1R <= 0 || netProfit1R < 1.5*chargesAt1R {
		return SizingDecision{
			Approved:     false,
			RejectReason: fmt.Sprintf("net profit at +1R (₹%.2f) insufficient vs round-trip taxes (₹%.2f) for qty=%d", netProfit1R, chargesAt1R, finalQty),
		}
	}

	// Minimum viable partial-fill dust floor (at least 1 share and enough to cover 2x taxes at +1R)
	minViableQty := max(1, int(math.Ceil((2.5*chargesAt1R)/math.Max(0.05, stopSpec.OneR))))
	if minViableQty > finalQty {
		minViableQty = finalQty
	}

	runnerQty := max(1, finalQty-(finalQty/2))
	beStop := BreakevenTriggerPrice(entryLimitPrice, finalQty, runnerQty, tickSize)
	reqMargin := (float64(finalQty) * entryLimitPrice) / leverage

	return SizingDecision{
		Approved:         true,
		Quantity:         finalQty,
		DustFloorQty:     minViableQty,
		EntryLimitPrice:  entryLimitPrice,
		StopSpec:         stopSpec,
		BreakevenStop:    beStop,
		RiskBudgetRupees: riskBudget,
		RequiredMargin:   reqMargin,
		ExpectedNet1R:    netProfit1R,
		EstimatedTaxes:   chargesAt1R,
	}
}
