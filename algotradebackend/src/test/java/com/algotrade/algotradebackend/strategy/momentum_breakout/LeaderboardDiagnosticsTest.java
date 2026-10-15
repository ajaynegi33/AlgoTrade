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
    void testUninitializedConstituentFailsGracefullyWithoutCrashing() {
        CandidateMetrics raw = new CandidateMetrics("UNKNOWN");
        assertEquals(0.0, raw.getGapPct());
        assertEquals(0, raw.getWindowVolume());

        MomentumScoringEngine scoringEngine = new MomentumScoringEngine();
        ScoredCandidate scored = scoringEngine.evaluate(raw, LocalTime.of(10, 0));
        assertFalse(scored.isQualified());
        assertEquals("DISQUALIFIED", scored.status());
    }
}
