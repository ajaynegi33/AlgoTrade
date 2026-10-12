package com.algotrade.algotradebackend.strategy.momentum_breakout;

import com.algotrade.algotradebackend.core.model.ScoredCandidate;
import org.junit.jupiter.api.Test;

import java.util.List;

import static org.junit.jupiter.api.Assertions.*;

/**
 * Runnable self-check for the 100-point Momentum Scoring Engine.
 */
public class MomentumScoringEngineTest {

    private final MomentumScoringEngine engine = new MomentumScoringEngine();

    @Test
    void testScoringAndDeterministicRanking() {
        // Candidate 1: Strong Breakout (RVOL >= 3.0, Price above VWAP by 1%, High Buy Depth, Breaks PDH with high ATR)
        CandidateMetrics c1 = new CandidateMetrics("TATAMOTORS", 700.0, 710.0, 10_000_000, 2.5);
        // 09:15 opening tick: initial volume = 100,000
        c1.updateTick(715.0, 100_000, 2_000_000, 500_000);
        // Most volume trades at 715.0
        c1.updateTick(715.0, 3_000_000, 2_000_000, 500_000);
        // Breakout push to 720.0: Window Vol = 3.0M (RVOL = 3.0 -> 30 pts), VWAP = 715.16, VD = 0.67% -> 25 pts
        c1.updateTick(720.0, 3_100_000, 2_000_000, 500_000);
        // LTP = 720 > PDH (710) with ATR 2.5% >= 1.8% -> 20 pts

        ScoredCandidate score1 = engine.evaluate(c1);
        assertEquals(30, score1.rvolScore());
        assertEquals(25, score1.vwapScore());
        assertEquals(25, score1.depthImbalanceScore());
        assertEquals(20, score1.pdhScore());
        assertEquals(100, score1.totalScore());
        assertTrue(score1.isQualified());

        // Candidate 2: Weak / Trap Candidate (Price below VWAP, Sellers dominate depth)
        CandidateMetrics c2 = new CandidateMetrics("WEAKSTOCK", 100.0, 105.0, 5_000_000, 1.2);
        c2.updateTick(102.0, 1_000_000, 100_000, 900_000);
        c2.updateTick(99.0, 1_200_000, 100_000, 900_000); // Price drops below open/VWAP

        ScoredCandidate score2 = engine.evaluate(c2);
        assertEquals(0, score2.vwapScore(), "Price below VWAP must yield 0 points");
        assertEquals(0, score2.depthImbalanceScore(), "Seller-dominated depth must yield 0 points");
        assertFalse(score2.isQualified());

        // Test Ranking & Tie-Breaker
        List<ScoredCandidate> ranked = engine.rankCandidates(List.of(score2, score1));
        assertEquals(1, ranked.get(0).rank());
        assertEquals("TATAMOTORS", ranked.get(0).symbol());
        assertEquals("SELECTED FOR TRADE", ranked.get(0).status());
        assertEquals(2, ranked.get(1).rank());
        assertEquals("WEAKSTOCK", ranked.get(1).symbol());
    }
}
