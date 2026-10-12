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

    @Test
    void testHardGateDisqualifiesHighScoringCandidateWithoutPDHBreakout() {
        // Candidate: RVOL=30, VWAP=25, Depth=25 -> Total Score = 80!
        // BUT Price (705) has NOT broken Previous Day High (710)!
        CandidateMetrics candidate = new CandidateMetrics("NOBREAKOUT", 700.0, 710.0, 10_000_000, 2.5);
        candidate.updateTick(701.0, 100_000, 2_000_000, 500_000);
        candidate.updateTick(704.0, 3_000_000, 2_000_000, 500_000);
        candidate.updateTick(707.0, 3_100_000, 2_000_000, 500_000); // LTP 707 < PDH 710, VD ~0.42% (25 pts)

        ScoredCandidate score = engine.evaluate(candidate);
        assertEquals(80, score.totalScore());
        assertFalse(score.eligible(), "Candidate without PDH breakout must have eligible=false");
        assertEquals("DISQUALIFIED", score.status());
        assertFalse(score.isQualified(), "Candidate with score 80 must be disqualified if no PDH breakout occurs");

        // Verify that an eligible candidate with lower score (75) is prioritized over an ineligible candidate with higher score (80)
        CandidateMetrics valid75 = new CandidateMetrics("VALID75", 100.0, 102.0, 10_000_000, 2.0);
        valid75.updateTick(103.0, 100_000, 2_000_000, 500_000);
        valid75.updateTick(103.0, 2_500_000, 2_000_000, 500_000); // RVOL ~2.4 (20 pts), VWAP 103 (VD 0% -> 0 pts), Depth (25 pts), PDH 103>102 (20 pts)
        valid75.updateTick(103.5, 3_100_000, 2_000_000, 500_000); // RVOL 30, VD 0.48% (25 pts), PDH 20 -> qualified
        ScoredCandidate validScore = engine.evaluate(valid75);
        assertTrue(validScore.eligible());
        assertTrue(validScore.isQualified());

        List<ScoredCandidate> ranked = engine.rankCandidates(List.of(score, validScore));
        // Valid candidate must be rank 1 despite score comparison
        assertEquals("VALID75", ranked.get(0).symbol());
        assertEquals("SELECTED FOR TRADE", ranked.get(0).status());
        assertTrue(ranked.get(0).isQualified());

        assertEquals("NOBREAKOUT", ranked.get(1).symbol());
        assertEquals("DISQUALIFIED", ranked.get(1).status());
        assertFalse(ranked.get(1).isQualified());
    }
}
