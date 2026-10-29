package trading

import (
	"fmt"
	"math"

	"github.com/ajaynegi33/AlgoTrade/internal/market"
)

// =========================================================================================
// POSITION SIZING & CAPITAL ALLOCATION CONFIGURATION CHEAT-SHEET
// =========================================================================================
// Edit these constants to tune capital risk budgets, margin rules, and concentration limits:
const (
	// DefaultConfiguredRiskPct is the standard risk per trade (0.50% of Net Equity).
	DefaultConfiguredRiskPct = 0.0050

	// SmallAccountEquityThreshold marks accounts (≤ ₹15,000) that automatically scale
	// to 1.25% risk so that position sizes clear NSE statutory charges and utilize 5x MIS leverage.
	SmallAccountEquityThreshold = 15000.0

	// MidAccountEquityThreshold marks accounts (≤ ₹50,000) that scale to 1.00% risk.
	MidAccountEquityThreshold = 50000.0

	// UpperMidAccountEquityThreshold marks accounts (≤ ₹2,00,000) that scale to 0.75% risk.
	UpperMidAccountEquityThreshold = 200000.0

	// LargeAccountEquityThreshold marks institutional accounts (> ₹50,00,000) that taper to 0.35% risk.
	LargeAccountEquityThreshold = 5000000.0

	// StandardConcentrationRatio is the maximum fraction of Net Equity deployed in a single stock (35%).
	StandardConcentrationRatio = 0.35

	// MicroConcentrationRatio is the relaxed single-stock exposure for micro accounts < ₹25k (95%).
	MicroConcentrationRatio = 0.95

	// MaxTop3AskLiquidityRatio prevents market impact by capping order size at 20% of top-3 ask depth.
	MaxTop3AskLiquidityRatio = 0.20

	// NetProfitGateThresholdMult enforces that net profit at +1.0R must be at least 1.5x
	// total round-trip statutory taxes & brokerage, otherwise the trade is rejected.
	NetProfitGateThresholdMult = 1.50
)

// =========================================================================================
// DOMAIN MODELS
// =========================================================================================

// SizingDecision holds the output of the 4-Constraint Position Sizer and Net-Profitability Gate
type SizingDecision struct {
	IsApproved          bool              `json:"approved"`
	RejectReason        string            `json:"reject_reason,omitempty"`
	Quantity            int               `json:"quantity"`
	DustFloorQty        int               `json:"dust_floor_qty"`
	EntryLimitPrice     float64           `json:"entry_limit_price"`
	StopSpec            StopAndBufferSpec `json:"stop_spec"`
	BreakevenStop       float64           `json:"breakeven_stop"`
	TargetR1Price       float64           `json:"target_r1_price"`
	TargetR2Price       float64           `json:"target_r2_price"`
	RiskRewardRatio     float64           `json:"risk_reward_ratio"`
	BreakevenWinRatePct float64           `json:"breakeven_win_rate_pct"`
	RiskBudgetRupees    float64           `json:"risk_budget_rupees"`
	RequiredMargin      float64           `json:"required_margin"`
	ExpectedNet1R       float64           `json:"expected_net_1r"`
	EstimatedTaxes      float64           `json:"estimated_taxes"`
}

// =========================================================================================
// CORE SIZING CALCULATIONS
// =========================================================================================

// EffectiveRiskPerTradePct implements the Scale-Invariant Adaptive Risk Curve across ₹500 to ₹10Cr+ accounts.
//
// Logic:
//  - If user sets a custom override (between 0.01% and 3.0%), honor it directly.
//  - Accounts ≤ ₹15k (e.g. ₹10k with 5x MIS): scale to 1.25% (₹125 budget) so 5x buying power is utilized.
//  - Accounts ≤ ₹50k: 1.00%
//  - Accounts ≤ ₹2L: 0.75%
//  - Accounts ≤ ₹50L: 0.50%
//  - Institutional (> ₹50L to ₹10Cr+): 0.35% for capital preservation.
func EffectiveRiskPerTradePct(netEquity, configuredRiskPct float64) float64 {
	if configuredRiskPct > 0 && configuredRiskPct <= 0.03 && math.Abs(configuredRiskPct-DefaultConfiguredRiskPct) > 1e-6 {
		return configuredRiskPct
	}
	switch {
	case netEquity <= SmallAccountEquityThreshold:
		return 0.0125 // 1.25% (₹125 on ₹10k equity -> cleanly utilizes 5x MIS buying power)
	case netEquity <= MidAccountEquityThreshold:
		return 0.0100 // 1.00%
	case netEquity <= UpperMidAccountEquityThreshold:
		return 0.0075 // 0.75%
	case netEquity <= LargeAccountEquityThreshold:
		return 0.0050 // 0.50%
	default:
		return 0.0035 // 0.35% for ₹50L to ₹10Cr+
	}
}

// ComputePositionSize evaluates the 4 institutional sizing constraints (Risk, Margin, Concentration, Liquidity)
// from ₹500 to ₹10Cr+ accounts and enforces the Mandatory Net-Profitability Gate at +1.0R.
func ComputePositionSize(
	stockMetrics *market.StockMetrics,
	signalBarLow float64,
	netEquity float64,
	freeMargin float64,
	riskPerTradePct float64,
	sessionSizeMult float64,
	drawdownMult float64,
) SizingDecision {
	tickSize := float64(stockMetrics.TickSize)
	if tickSize <= 0 {
		tickSize = 0.05
	}

	// 1. Marketable limit entry price: BestAsk + 1 tick (or LTP + 1 tick if Ask1 is 0)
	refAsk := float64(stockMetrics.Ask1Price)
	if refAsk <= 0 || refAsk < stockMetrics.LTP {
		refAsk = stockMetrics.LTP
	}
	entryLimitPrice := RoundUpToTick(refAsk+tickSize, tickSize)

	// 2. Delegate Stop Loss & 1R calculation to stoploss.go
	stopSpec, err := ComputeStopAndBufferSpec(entryLimitPrice, signalBarLow, stockMetrics.ATR14, tickSize)
	if err != nil {
		return SizingDecision{
			IsApproved:   false,
			RejectReason: err.Error(),
		}
	}

	// 3. Minimum Capital Guard
	if netEquity < 500.0 || freeMargin <= 0 {
		return SizingDecision{
			IsApproved:   false,
			RejectReason: fmt.Sprintf("insufficient capital (NetEquity=₹%.2f, FreeMargin=₹%.2f)", netEquity, freeMargin),
		}
	}

	effectiveRiskPct := EffectiveRiskPerTradePct(netEquity, riskPerTradePct)
	if sessionSizeMult <= 0 {
		sessionSizeMult = 1.0
	}
	if drawdownMult <= 0 {
		drawdownMult = 1.0
	}

	// 4. Compute Risk Budget in Rupees
	riskBudget := netEquity * effectiveRiskPct * sessionSizeMult * drawdownMult
	// Small-Account Floor (₹500 to ₹25,000): prevent session/drawdown multipliers from starving 5x MIS sizing
	if netEquity <= 25000.0 {
		riskBudget = math.Max(riskBudget, netEquity*0.0075)
	}

	leverage := float64(stockMetrics.MISLeverage)
	if leverage < 1.0 || leverage > 5.0 {
		leverage = 5.0
	}

	// Estimate per-share tax + slippage friction at tentative quantity
	tentativeQty := max(1, int(math.Floor(riskBudget/stopSpec.OneR)))
	estimatedCharges := CalculateRoundTripCharges(entryLimitPrice, stopSpec.InitialStop, tentativeQty)
	perShareFriction := (estimatedCharges.TotalCharges / float64(tentativeQty)) + 1.5*tickSize

	// =========================================================================
	// THE 4 INSTITUTIONAL SIZING CONSTRAINTS
	// =========================================================================

	// Constraint 1: Risk-Capped Quantity
	riskCappedQuantity := int(math.Floor(riskBudget / (stopSpec.OneR + perShareFriction)))

	// Constraint 2: Margin-Capped Quantity (with 5% RMS safety buffer)
	marginCappedQuantity := int(math.Floor((0.95 * freeMargin * leverage) / entryLimitPrice))

	// Constraint 3: Single-Stock Concentration Cap
	concentrationFactor := StandardConcentrationRatio
	if netEquity < 25000.0 {
		concentrationFactor = MicroConcentrationRatio
	}
	concentrationCappedQuantity := int(math.Floor((concentrationFactor * netEquity * leverage) / entryLimitPrice))

	// Constraint 4: Order-Book Top-3 Ask Liquidity Cap (never sweep more than 20% of visible depth)
	var askTop3Qty uint64
	for i := 0; i < 3; i++ {
		askTop3Qty += uint64(stockMetrics.AskLevels[i].Qty)
	}
	liquidityCappedQuantity := max(1, int(math.Floor(MaxTop3AskLiquidityRatio*float64(askTop3Qty))))

	// Micro-Capital Allowance (₹500 to ₹25,000 accounts):
	// If riskCappedQuantity == 0 because 0.5% of ₹1,000 is ₹5, allow 1 share if margin covers it and max loss <= 2.0% of NetEquity
	if riskCappedQuantity == 0 && netEquity < 25000.0 && marginCappedQuantity >= 1 {
		if (stopSpec.OneR + perShareFriction) <= 0.02*netEquity {
			riskCappedQuantity = 1
		}
	}

	// Final Allocated Quantity = Min across all 4 constraints
	allocatedQuantity := min(riskCappedQuantity, min(marginCappedQuantity, min(concentrationCappedQuantity, liquidityCappedQuantity)))
	if allocatedQuantity <= 0 {
		return SizingDecision{
			IsApproved:   false,
			RejectReason: fmt.Sprintf("quantity constrained to 0 (qRisk=%d, qMargin=%d, qConc=%d, qLiq=%d)", riskCappedQuantity, marginCappedQuantity, concentrationCappedQuantity, liquidityCappedQuantity),
		}
	}

	// =========================================================================
	// MANDATORY NET-PROFITABILITY GATE AT +1.0R
	// =========================================================================
	// Net profit at +1.0R must be at least 1.5x round-trip statutory & brokerage charges
	target1RPrice := entryLimitPrice + stopSpec.OneR
	chargesAt1R := CalculateRoundTripCharges(entryLimitPrice, target1RPrice, allocatedQuantity).TotalCharges
	grossProfit1R := float64(allocatedQuantity) * stopSpec.OneR
	netProfit1R := grossProfit1R - chargesAt1R

	if netProfit1R <= 0 || netProfit1R < NetProfitGateThresholdMult*chargesAt1R {
		return SizingDecision{
			IsApproved: false,
			RejectReason: fmt.Sprintf(
				"net profit at +1R (₹%.2f) insufficient vs round-trip taxes (₹%.2f) for qty=%d",
				netProfit1R, chargesAt1R, allocatedQuantity,
			),
		}
	}

	// Minimum viable partial-fill dust floor (at least 1 share and enough to cover 2x taxes at +1R)
	minViableQty := max(1, int(math.Ceil((2.5*chargesAt1R)/math.Max(0.05, stopSpec.OneR))))
	if minViableQty > allocatedQuantity {
		minViableQty = allocatedQuantity
	}

	// Calculate Breakeven Stop on runner tranche using stoploss.go
	runnerQty := max(1, allocatedQuantity-(allocatedQuantity/2))
	beStop := BreakevenTriggerPrice(entryLimitPrice, allocatedQuantity, runnerQty, tickSize)
	reqMargin := (float64(allocatedQuantity) * entryLimitPrice) / leverage

	// Target Projections: R1 target at +0.65R (scale-out), R2 target at +1.65R (runner expansion)
	targetR1 := RoundUpToTick(entryLimitPrice+0.65*stopSpec.OneR, tickSize)
	targetR2 := RoundUpToTick(entryLimitPrice+1.65*stopSpec.OneR, tickSize)
	riskRewardRatio := RoundToTick((targetR2-entryLimitPrice)/stopSpec.OneR, 0.01)
	beWinRate := math.Round((stopSpec.OneR/(stopSpec.OneR+(targetR2-entryLimitPrice)))*1000.0) / 10.0

	return SizingDecision{
		IsApproved:          true,
		Quantity:            allocatedQuantity,
		DustFloorQty:        minViableQty,
		EntryLimitPrice:     entryLimitPrice,
		StopSpec:            stopSpec,
		BreakevenStop:       beStop,
		TargetR1Price:       targetR1,
		TargetR2Price:       targetR2,
		RiskRewardRatio:     riskRewardRatio,
		BreakevenWinRatePct: beWinRate,
		RiskBudgetRupees:    riskBudget,
		RequiredMargin:      reqMargin,
		ExpectedNet1R:       netProfit1R,
		EstimatedTaxes:      chargesAt1R,
	}
}
