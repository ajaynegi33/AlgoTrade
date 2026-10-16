package com.algotrade.algotradebackend.strategy.momentum_breakout;

import com.algotrade.algotradebackend.backtest.model.BacktestRequest;
import com.algotrade.algotradebackend.core.model.ScoredCandidate;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;

import java.time.LocalTime;

import static org.junit.jupiter.api.Assertions.*;

/**
 * Authoritative verification suite for Phase 2: Backtest Engine Realism & Data Provenance.
 * Verifies:
 * 1. Backtest VWAP eliminates double-counting and equals exact typical price accumulation.
 * 2. Backtest scoring qualification threshold is calibrated to 80 points.
 * 3. passesHardGates strictly rejects overextended candidates (> 2.2% above VWAP).
 * 4. BacktestRequest defaults maxDailyLossRupees to 400.0 with backward-compatible constructors.
 */
class Phase2BacktestRealismTest {

    private final MomentumScoringEngine scoringEngine = new MomentumScoringEngine();

    @Test
    @DisplayName("1. VWAP does not double count volume or price-volume products")
    void test1_VwapDoesNotDoubleCountVolume() {
        CandidateMetrics m = new CandidateMetrics("TESTSYM", 100.0, 105.0, 1_000_000, 2.0);

        // Candle 1: 09:15-09:20. O=100, H=102, L=99, C=101, V=1000
        // Typical price = (102 + 99 + 101) / 3 = 100.666667
        m.aggregateCandle(100.0, 102.0, 99.0, 101.0, 1000, true);

        assertEquals(1000, m.getWindowVolume(), "Window volume after bar 1 must equal exact bar volume (1,000)");
        assertEquals(100.666667, m.getVwap(), 0.001, "VWAP after bar 1 must equal bar 1 typical price");

        // Candle 2: 09:20-09:25. O=101, H=104, L=100, C=103, V=2000
        // Typical price = (104 + 100 + 103) / 3 = 102.333333
        m.aggregateCandle(101.0, 104.0, 100.0, 103.0, 2000, true);

        // Expected cumulative volume: 1000 + 2000 = 3000 (NOT 6000)
        assertEquals(3000, m.getWindowVolume(), "Window volume must not be doubled; must equal 3,000");

        // Expected cumulative typical price * volume = (100.666667 * 1000) + (102.333333 * 2000) = 100666.67 + 204666.67 = 305333.34
        // Expected VWAP = 305333.34 / 3000 = 101.777778
        assertEquals(101.777778, m.getVwap(), 0.001, "VWAP must strictly match typical price accumulation without tick double-counting");
    }

    @Test
    @DisplayName("2. Calibrated scoring threshold rejects 55-pt candidate and qualifies 60-pt candidate in backtest")
    void test2_CalibratedScoringThresholdInBacktestMode() {
        // Candidate 55 raw points:
        // RVOL 20 pts (RVOL 2.2x), VWAP 25 pts (ideal expansion 1.0%), Depth 0 (backtest), PDH 10 pts (breakout without HOD / high ATR)
        // Raw non-depth sum = 20 + 25 + 10 = 55 pts.
        // 55 * (100.0 / 75.0) = 73.33 -> 73 pts.
        CandidateSnapshot s55 = new CandidateSnapshot(
            "CAND55",
            101.0,  // ltp
            100.0,  // openPrice
            99.5,   // effectiveLow
            105.0,  // high15 (LTP 101.0 < 105.0, so no HOD breakout)
            100.0,  // prevClose
            100.5,  // previousDayHigh (LTP 101.0 > 100.5 -> PDH breakout)
            1_000_000, // avgDailyVolume10Day
            1.2,    // atrPct (< 1.8%, so PDH score = 10 pts)
            220_000, // windowVol (RVOL ~ 2.2 -> 20 pts)
            100.0,  // vwap (VD = (101-100)/100 = 1.0% -> 25 pts)
            0.0,    // depthImbalance
            0.0     // gapPct
        );

        ScoredCandidate score55 = scoringEngine.evaluate(s55, LocalTime.of(9, 30), true);
        assertEquals(73, score55.totalScore());
        // Under calibrated 80-point threshold, a 73-score candidate is strictly DISQUALIFIED
        assertFalse(score55.isQualified(), "55 raw point candidate (score 73) must be DISQUALIFIED under calibrated 80 threshold");
        assertEquals("DISQUALIFIED", score55.status());

        // Candidate 60 raw points:
        // RVOL 20 pts, VWAP 25 pts, PDH 15 pts (breakout with ATR >= 1.8%, no HOD breakout)
        // Raw non-depth sum = 20 + 25 + 15 = 60 pts.
        // 60 * (100 / 75) = 80 pts!
        CandidateSnapshot s60 = new CandidateSnapshot(
            "CAND60",
            101.0,
            100.0,
            99.5,
            105.0,  // high15 (LTP 101.0 < 105.0, so no HOD breakout -> 15 pts)
            100.0,
            100.5,
            1_000_000,
            2.0,    // atrPct >= 1.8% -> PDH score = 15 pts
            220_000,
            100.0,
            0.0,
            0.0
        );

        ScoredCandidate score60 = scoringEngine.evaluate(s60, LocalTime.of(9, 30), true);
        assertEquals(80, score60.totalScore());
        assertTrue(score60.isQualified(), "60 raw point candidate (score 80) must be QUALIFIED under calibrated 80 threshold");
        assertEquals("QUALIFIED", score60.status());
    }

    @Test
    @DisplayName("3. Hard gate strictly rejects candidate overextended > 2.2% above VWAP")
    void test3_OverextendedVwapHardGateRejection() {
        // Candidate with LTP = 102.50, VWAP = 100.0 -> Extension = +2.50% (> 2.20% ceiling)
        CandidateSnapshot overextended = new CandidateSnapshot(
            "OVEREXTENDED",
            102.50, // ltp
            100.0,
            99.5,
            101.5,
            100.0,
            101.0,
            1_000_000,
            2.0,
            350_000,
            100.0,  // vwap
            0.0,
            0.0
        );

        assertFalse(scoringEngine.passesHardGates(overextended),
            "Stock trading > 2.2% above VWAP must fail hard gates as an overextended mean-reversion trap");

        ScoredCandidate score = scoringEngine.evaluate(overextended, LocalTime.of(9, 30), true);
        assertFalse(score.eligible());
        assertFalse(score.isQualified());
        assertEquals("DISQUALIFIED", score.status());

        // Candidate with LTP = 101.50, VWAP = 100.0 -> Extension = +1.50% (<= 2.20% ceiling)
        CandidateSnapshot valid = new CandidateSnapshot(
            "VALID",
            101.50, // ltp
            100.0,
            99.5,
            101.0,
            100.0,
            101.0,
            1_000_000,
            2.0,
            350_000,
            100.0,  // vwap
            0.0,
            0.0
        );

        assertTrue(scoringEngine.passesHardGates(valid), "Stock trading <= 2.2% above VWAP must pass hard gates");
    }

    @Test
    @DisplayName("4. BacktestRequest defaults maxDailyLossRupees to 400.0 and supports backward compatible constructors")
    void test4_BacktestRequestCircuitBreakerDefaults() {
        // 7-arg constructor
        BacktestRequest req7 = new BacktestRequest("2026-07-01", "2026-09-30", 10000.0, 1.0, 75, 0.8, 0.05);
        assertEquals(400.0, req7.maxDailyLossRupees(), "7-arg constructor must default maxDailyLossRupees to 400.0");
        assertEquals(1.0, req7.leverageMultiplier());

        // 8-arg constructor
        BacktestRequest req8 = new BacktestRequest("2026-07-01", "2026-09-30", 10000.0, 1.0, 75, 0.8, 0.05, 5.0);
        assertEquals(400.0, req8.maxDailyLossRupees(), "8-arg constructor must default maxDailyLossRupees to 400.0");
        assertEquals(5.0, req8.leverageMultiplier());

        // 9-arg canonical constructor with explicit override
        BacktestRequest req9 = new BacktestRequest("2026-07-01", "2026-09-30", 10000.0, 1.0, 75, 0.8, 0.05, 5.0, 500.0);
        assertEquals(500.0, req9.maxDailyLossRupees(), "Canonical constructor must accept explicit maxDailyLossRupees override");

        // Null / negative fallback
        BacktestRequest reqFallback = new BacktestRequest(null, null, null, null, null, null, null, null, -50.0);
        assertEquals(400.0, reqFallback.maxDailyLossRupees(), "Negative or null maxDailyLossRupees must fallback to 400.0");
    }
}
