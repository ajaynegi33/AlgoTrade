package com.algotrade.algotradebackend.strategy.momentum_breakout;

import com.algotrade.algotradebackend.core.model.ScoredCandidate;
import org.junit.jupiter.api.Test;

import java.time.LocalTime;

import static org.junit.jupiter.api.Assertions.*;

public class LeaderboardDiagnosticsTest {

    @Test
    void testContinuousScanConstituentHydratesReferenceDataAndComputesAccurateGap() {
        CandidateMetrics m = new CandidateMetrics("JSWSTEEL");
        assertEquals(0.0, m.getPrevClose());
        assertEquals(0.0, m.getGapPct());

        // Continuous scanner updates reference data and official pre-open open price
        double prevClose = 1175.0;
        double pdh = 1180.0;
        double avgVol = 3_000_000.0;
        double atrPct = 2.0;
        double officialOpenPrice = 1185.0; // Discovered during 09:00-09:08 pre-open
        m.setReferenceData(prevClose, pdh, avgVol, atrPct, officialOpenPrice);

        // Gap should be ((1185.0 - 1175.0) / 1175.0) * 100 = +0.851%
        assertEquals(0.851, m.getGapPct(), 0.01, "Pre-open gap must be accurately calculated from official open");
        assertEquals(1185.0, m.getOpenPrice());
        assertEquals(1180.0, m.getPreviousDayHigh());
    }

    @Test
    void testContinuousScanCandidateRvolCalibrationMidDay() {
        MomentumScoringEngine scoringEngine = new MomentumScoringEngine();

        CandidateMetrics m = new CandidateMetrics("JSWSTEEL");
        double prevClose = 1175.0;
        double pdh = 1180.0;
        double avgVol = 2_000_000.0;
        double atrPct = 2.0;
        double officialOpenPrice = 1185.0;

        m.setReferenceData(prevClose, pdh, avgVol, atrPct, officialOpenPrice);
        // Continuous scan establishes session volume baseline at 0
        m.setOpeningVolumeBaseline(0);

        // At 10:15 AM (60 minutes into the session), 650,000 shares have traded
        // LTP is 1186.0 (breaking above PDH 1180.0), VWAP is 1180.50 (+0.46% above VWAP)
        // Depth: totalBuy = 70,000, totalSell = 30,000 (imbalance = +0.40 >= 0.35 -> 25 pts)
        LocalTime scanTime = LocalTime.of(10, 15);
        m.updateTick(1186.0, 650_000, 70_000, 30_000, 1180.50, false);

        assertEquals(650_000, m.getWindowVolume(), "Window volume must capture cumulative day volume when baseline is 0");

        ScoredCandidate scored = scoringEngine.evaluate(m, scanTime);

        // Expected vol at 10:15 AM = 2,000,000 * (60 / 375) = 320,000 shares
        // RVOL = 650,000 / 320,000 = 2.03125 >= 2.00 -> 20 RVOL points!
        assertEquals(20, scored.rvolScore(), "RVOL score must award points for high intraday volume");
        assertEquals(25, scored.vwapScore(), "VWAP score must be 25 for ideal +0.46% expansion");
        assertEquals(25, scored.depthImbalanceScore(), "Depth score must be 25 for +0.40 imbalance");
        assertTrue(scored.pdhScore() >= 10, "PDH score must award points since LTP (1186) > PDH (1180)");

        // Total score: 20 (RVOL) + 25 (VWAP) + 25 (Depth) + 15/20 (PDH) >= 80 points
        assertTrue(scored.totalScore() >= 80, "Candidate must score >= 80 points");
        assertTrue(scored.isQualified(), "Candidate must pass qualification threshold (>= 75 PTS)");
        assertEquals("QUALIFIED", scored.status());
    }

    @Test
    void testRedStockOrNegativeGapCannotPassHardGates() {
        MomentumScoringEngine scoringEngine = new MomentumScoringEngine();

        // Stock like SRF: Previous Close = 2499.75, Open = 2499.0 (-0.03% gap), LTP = 2495.0 (-0.19% down on day)
        CandidateMetrics m = new CandidateMetrics("SRF");
        m.setReferenceData(2499.75, 2490.0, 1_000_000.0, 2.0, 2499.0);
        m.setOpeningVolumeBaseline(0);
        m.updateTick(2495.0, 500_000, 30_000, 10_000, 2493.0, false);

        CandidateSnapshot snapshot = m.getSnapshot();
        // Even though LTP (2495) > synthetic PDH (2490), the stock is RED on the day (2495 < 2499.75)
        assertFalse(scoringEngine.passesHardGates(snapshot), "A stock trading below previous close must fail hard gates");

        ScoredCandidate scored = scoringEngine.evaluate(m, LocalTime.of(10, 15));
        assertFalse(scored.isQualified(), "Red stock must be disqualified regardless of score");
        assertEquals("DISQUALIFIED", scored.status());
    }

    @Test
    void testHeroMotoCoPositionSizingPassesWithNoiseFloorStop() {
        double entryPrice = 5163.0;
        double tickSize = 0.05;
        double accountCapital = 10000.0;
        double riskBudgetPct = 2.0;
        double maxExposureMultiplier = 5.0;
        double minStopDistancePct = 0.35;
        double maxStopDistancePct = 2.0;

        // Case A: Day's low equals entry price (e.g. freshly reset session)
        double effLow15 = entryPrice;
        if (effLow15 <= 0 || effLow15 >= entryPrice) {
            effLow15 = com.algotrade.algotradebackend.core.util.TickMath.floorToTick(entryPrice * 0.995, tickSize);
        }
        assertEquals(5137.15, effLow15, 0.01);

        double supportDistPct = ((entryPrice - effLow15) / entryPrice) * 100.0;
        assertTrue(supportDistPct <= 1.5, "Support distance within 1.5% cap");

        double maxAllowedStop = com.algotrade.algotradebackend.core.util.TickMath.floorToTick(entryPrice * 0.992, tickSize);
        double noiseFloorStop = com.algotrade.algotradebackend.core.util.TickMath.floorToTick(entryPrice * 0.9955, tickSize);
        double calculatedStop = (supportDistPct < 0.45)
            ? noiseFloorStop
            : com.algotrade.algotradebackend.core.util.TickMath.floorToTick(effLow15 - tickSize, tickSize);
        double initialStop = com.algotrade.algotradebackend.core.util.TickMath.floorToTick(Math.max(calculatedStop, maxAllowedStop), tickSize);

        assertTrue(initialStop > 0 && initialStop < entryPrice, "Stop must be strictly positive and below entry");
        double stopDistPct = ((entryPrice - initialStop) / entryPrice) * 100.0;
        assertTrue(stopDistPct >= minStopDistancePct && stopDistPct <= maxStopDistancePct,
            "Stop distance % (" + stopDistPct + "%) must be within allowable bounds [" + minStopDistancePct + "%, " + maxStopDistancePct + "%]");

        int qty = com.algotrade.algotradebackend.risk.PositionSizingEngine.calculateSizingQuantity(
            accountCapital, entryPrice, initialStop, riskBudgetPct, maxExposureMultiplier, minStopDistancePct, maxStopDistancePct
        );
        assertTrue(qty > 0, "Calculated quantity must be positive (got: " + qty + ")");
        assertEquals(7, qty, "HEROMOTOCO with ₹10,000 capital and 2% risk should size to 7 shares (risk: ₹181.30 <= ₹200)");
    }
}
