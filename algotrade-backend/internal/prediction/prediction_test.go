package prediction

import (
	"testing"

	"github.com/ajaynegi33/AlgoTrade/internal/engine"
)

func TestOrderBlockAndMinimaxAlphaBeta(t *testing.T) {
	stock := &engine.StockMetrics{
		InstrumentToken: 779521,
		LTP:             842.10,
		TickSize:        0.05,
		ATR14:           6.00,
		ExchangeVWAP:    840.00,
		AggressorBuyRatio: 0.65,
		FastAggressorEMA:  0.62,
		SmoothedBuyerRatio: 0.60,
		RVOLSession:     2.10,
		CurrentBarIdx:   4,
	}
	copy(stock.Symbol[:], []byte("SBIN"))

	// 5m Bars: Bar 1 Base, Bar 2 Impulse breakout
	stock.SessionBars[0] = engine.OHLCVBar{Open: 838.0, High: 840.5, Low: 837.5, Close: 839.0, Volume: 10000}
	stock.SessionBars[1] = engine.OHLCVBar{Open: 839.0, High: 841.0, Low: 839.7, Close: 840.0, Volume: 15000} // OB Base
	stock.SessionBars[2] = engine.OHLCVBar{Open: 840.0, High: 846.5, Low: 839.8, Close: 846.0, Volume: 45000} // Impulse!
	stock.SessionBars[3] = engine.OHLCVBar{Open: 846.0, High: 846.8, Low: 843.5, Close: 844.0, Volume: 20000}
	stock.SessionBars[4] = engine.OHLCVBar{Open: 844.0, High: 844.2, Low: 841.5, Close: 842.1, Volume: 18000} // Pullback to Z!

	// 1. Test Bullish Order Block detection
	ob := DetectBullishOrderBlock(stock)
	if ob.Direction != "BULLISH" {
		t.Fatalf("expected BULLISH direction, got %s", ob.Direction)
	}
	if ob.ZoneLow <= 0 || ob.ZoneHigh <= ob.ZoneLow {
		t.Fatalf("invalid zone bounds: low=%.2f, high=%.2f", ob.ZoneLow, ob.ZoneHigh)
	}
	if ob.InvalidationS1 >= ob.ZoneLevelZ {
		t.Fatalf("invalidation S1 (%.2f) must be below Zone Z (%.2f)", ob.InvalidationS1, ob.ZoneLevelZ)
	}
	if ob.TargetR2 <= ob.TargetR1 {
		t.Fatalf("target R2 (%.2f) must be above Target R1 (%.2f)", ob.TargetR2, ob.TargetR1)
	}
	if ob.RiskRewardRatio < 1.4 {
		t.Fatalf("expected R:R >= 1.4, got %.2f", ob.RiskRewardRatio)
	}

	// 2. Test Minimax Alpha-Beta Evaluation
	eval := EvaluateMinimaxAlphaBeta(stock, true)
	if eval.ActionVerdict != "ACT_AT_ZONE_Z" {
		t.Fatalf("expected ACT_AT_ZONE_Z when price is near Z (842.10 vs Z=%.2f), got %s", ob.ZoneLevelZ, eval.ActionVerdict)
	}
	if eval.MinimaxEV <= 0 {
		t.Fatalf("expected positive Minimax EV, got %.2f", eval.MinimaxEV)
	}
	if len(eval.PredictedPath) == 0 {
		t.Fatalf("expected predicted path steps, got empty")
	}

	// 3. Test Chasing Detection when price is extended (LTP = 846.00)
	stock.LTP = 846.00
	chasingEval := EvaluateMinimaxAlphaBeta(stock, true)
	if chasingEval.ActionVerdict != "WAIT_FOR_PULLBACK_TO_Z" {
		t.Fatalf("expected WAIT_FOR_PULLBACK_TO_Z when chasing at 846.00, got %s", chasingEval.ActionVerdict)
	}
	if !chasingEval.OrderBlock.IsChasing {
		t.Fatalf("expected IsChasing to be true at 846.00")
	}
	if chasingEval.PrunedBranches < 1 {
		t.Fatalf("expected Alpha-Beta to prune chasing branches, got %d pruned", chasingEval.PrunedBranches)
	}
}
