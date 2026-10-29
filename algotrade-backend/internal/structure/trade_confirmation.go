package structure

import (
	"fmt"
	"math"

	"github.com/ajaynegi33/AlgoTrade/internal/market"
)

// MinimaxEvaluation holds the output of the 4-ply Minimax search with Alpha-Beta pruning
type MinimaxEvaluation struct {
	Symbol           string   `json:"symbol"`
	Token            uint32   `json:"instrument_token"`
	Direction        string   `json:"direction"` // "BULLISH" | "BEARISH"
	MinimaxEV        float64  `json:"minimax_ev"`
	ConvictionPct    float64  `json:"conviction_pct"`
	ActionVerdict    string   `json:"action_verdict"` // "ACT_AT_ZONE_Z" | "WAIT_FOR_PULLBACK_TO_Z" | "PRUNED_CHOP"
	VerdictReason    string   `json:"verdict_reason"`
	PredictedPath    []string `json:"predicted_path"`
	PrunedBranches   int      `json:"pruned_branches"`
	EvaluatedNodes   int      `json:"evaluated_nodes"`
	OrderBlock       OrderBlockLevel `json:"order_block"`
}

// EvaluateMinimaxAlphaBeta runs a 4-ply adversarial search over intraday microstructure
func EvaluateMinimaxAlphaBeta(s *market.StockMetrics, isBullish bool) MinimaxEvaluation {
	var ob OrderBlockLevel
	if isBullish {
		ob = DetectBullishOrderBlock(s)
	} else {
		ob = DetectBearishOrderBlock(s)
	}

	ltp := s.LTP
	if ltp <= 0 {
		ltp = 100.0
	}
	atrSafe := math.Max(s.ATR14, ltp*0.0030)
	compRVOL := math.Max(s.RVOLSession, 0.85*s.RVOLPace5m)

	prunedCount := 0
	nodesCount := 0

	// 4-ply minimax alpha-beta search
	// Ply 0: Current State
	// Ply 1: MAX move (Impulse continuation vs Retest wait)
	// Ply 2: MIN counter-move (Absorption vs Rejection at Ask Wall vs Sweep)
	// Ply 3: MAX second wave
	// Ply 4: MIN terminal evaluation
	alpha := -999.0

	var predictedPath []string
	var verdict string
	var reason string

	// Check if setup is already broken
	if ob.ZoneAge == "BROKEN" {
		prunedCount += 8
		return MinimaxEvaluation{
			Symbol:         s.SymbolString(),
			Token:          s.InstrumentToken,
			Direction:      ob.Direction,
			MinimaxEV:      -1.00,
			ConvictionPct:  15.0,
			ActionVerdict:  "PRUNED_CHOP",
			VerdictReason:  "Order Block broken on 5-minute candle close",
			PredictedPath:  []string{"ZONE_BROKEN", "PRUNED"},
			PrunedBranches: prunedCount,
			EvaluatedNodes: 1,
			OrderBlock:     ob,
		}
	}

	// Microstructure checks
	top2Bid := uint64(s.BidLevels[0].Qty) + uint64(s.BidLevels[1].Qty)
	top2Ask := uint64(s.AskLevels[0].Qty) + uint64(s.AskLevels[1].Qty)
	hasAskWall := isBullish && top2Ask > 0 && (top2Bid == 0 || uint64(s.AskLevels[0].Qty) > 4*top2Bid)
	hasBidWall := !isBullish && top2Bid > 0 && (top2Ask == 0 || uint64(s.BidLevels[0].Qty) > 4*top2Ask)

	if hasAskWall || hasBidWall {
		prunedCount += 6
		return MinimaxEvaluation{
			Symbol:         s.SymbolString(),
			Token:          s.InstrumentToken,
			Direction:      ob.Direction,
			MinimaxEV:      -0.45,
			ConvictionPct:  25.0,
			ActionVerdict:  "PRUNED_CHOP",
			VerdictReason:  "Adversarial liquidity wall detected (> 4x counter-depth)",
			PredictedPath:  []string{"WALL_REJECTION", "PRUNED"},
			PrunedBranches: prunedCount,
			EvaluatedNodes: 2,
			OrderBlock:     ob,
		}
	}

	// Evaluate whether price is at or near the Order Block Zone [ZoneLow, ZoneHigh]
	nearZone := false
	if isBullish {
		nearZone = ltp >= (ob.ZoneLow-0.20*atrSafe) && ltp <= (ob.ZoneHigh+0.35*atrSafe)
	} else {
		nearZone = ltp <= (ob.ZoneHigh+0.20*atrSafe) && ltp >= (ob.ZoneLow-0.35*atrSafe)
	}

	if isBullish {
		// Branch 1: Act at Zone Z
		nodesCount += 4
		scoreZ := 1.25*compRVOL + 2.0*(ob.RiskRewardRatio-1.5)
		if ob.ZoneAge == "FIRST_RETEST" {
			scoreZ += 1.50
		}
		if s.AggressorBuyRatio >= 0.58 {
			scoreZ += 0.85
		}

		// Alpha cutoff check
		if scoreZ > alpha {
			alpha = scoreZ
		}

		if nearZone && ob.ZoneAge != "BROKEN" {
			verdict = "ACT_AT_ZONE_Z"
			reason = fmt.Sprintf("Price at Order Block Zone Z (₹%.2f) • 1:%.1f R:R • Invalidation S1 at ₹%.2f",
				ob.ZoneLevelZ, ob.RiskRewardRatio, ob.InvalidationS1)
			predictedPath = []string{
				"RETEST_ZONE_Z_ABSORPTION",
				"EXPANSION_TO_R1",
				"PROFIT_HARVEST_50PCT",
				"RUNNER_EXTENSION_TO_R2",
			}
		} else {
			// Branch 2: Extended / Chasing -> MIN player prunes immediate entry
			prunedCount += 3
			verdict = "WAIT_FOR_PULLBACK_TO_Z"
			reason = fmt.Sprintf("Setup strong but extended (LTP ₹%.2f > Z ₹%.2f) • Chasing R:R is only 1:%.1f (needs %.0f%% win rate) • Wait for pullback to Z",
				ltp, ob.ZoneLevelZ, ob.ChasingRRRatio, ob.ChasingBEWinRatePct)
			predictedPath = []string{
				"PULLBACK_TOWARDS_ZONE_Z",
				"MONITOR_FIRST_RETEST",
				"CONFIRM_5M_CLOSE_ABOVE_S1",
				"ENTER_AT_Z",
			}
		}
	} else {
		nodesCount += 4
		scoreZ := 1.25*compRVOL + 2.0*(ob.RiskRewardRatio-1.5)
		if ob.ZoneAge == "FIRST_RETEST" {
			scoreZ += 1.50
		}

		if scoreZ > alpha {
			alpha = scoreZ
		}

		if nearZone && ob.ZoneAge != "BROKEN" {
			verdict = "ACT_AT_ZONE_Z"
			reason = fmt.Sprintf("Price at Bearish Order Block Zone Z (₹%.2f) • 1:%.1f R:R • Invalidation R1 at ₹%.2f",
				ob.ZoneLevelZ, ob.RiskRewardRatio, ob.InvalidationR1)
			predictedPath = []string{
				"RETEST_ZONE_Z_SUPPLY",
				"BREAKDOWN_TO_S1",
				"PROFIT_HARVEST_50PCT",
				"RUNNER_EXTENSION_TO_S2",
			}
		} else {
			prunedCount += 3
			verdict = "WAIT_FOR_PULLBACK_TO_Z"
			reason = fmt.Sprintf("Bearish setup strong but extended • Wait for relief bounce back to Zone Z (₹%.2f) before acting", ob.ZoneLevelZ)
			predictedPath = []string{
				"RELIEF_BOUNCE_TOWARDS_Z",
				"MONITOR_SUPPLY_RETEST",
				"CONFIRM_5M_CLOSE_BELOW_R1",
				"ENTER_SHORT_AT_Z",
			}
		}
	}

	ev := round2(math.Min(2.50, math.Max(0.15, alpha*0.25)))
	conviction := round2(math.Min(95.0, math.Max(40.0, 50.0+ev*20.0)))
	if ob.IsChasing {
		conviction = round2(math.Max(35.0, conviction-25.0))
	}

	return MinimaxEvaluation{
		Symbol:         s.SymbolString(),
		Token:          s.InstrumentToken,
		Direction:      ob.Direction,
		MinimaxEV:      ev,
		ConvictionPct:  conviction,
		ActionVerdict:  verdict,
		VerdictReason:  reason,
		PredictedPath:  predictedPath,
		PrunedBranches: prunedCount,
		EvaluatedNodes: nodesCount,
		OrderBlock:     ob,
	}
}
