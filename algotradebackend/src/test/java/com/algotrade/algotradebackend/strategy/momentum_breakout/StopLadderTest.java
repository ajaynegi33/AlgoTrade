package com.algotrade.algotradebackend.strategy.momentum_breakout;

import com.algotrade.algotradebackend.core.model.ChargesBreakdown;
import com.algotrade.algotradebackend.core.util.TickMath;
import com.algotrade.algotradebackend.risk.PositionSizingEngine;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;

import java.util.Optional;
import java.util.Random;

import static org.junit.jupiter.api.Assertions.*;

@DisplayName("StopLadder Mathematical Correctness & Invariant Tests")
class StopLadderTest {

    private static final double EPSILON = 0.0001;

    @Test
    @DisplayName("E1 Golden Vectors: TECHM Stage 0 and Stage 1 Candidate Verification")
    void testGoldenVectorsStage0AndStage1() {
        double entry = 1763.30;
        double s0 = 1748.30;
        double tick = 0.05;
        double bufferPct = 0.0015; // 0.15%

        // Stage 0
        StopLadder.StopOrderPair stage0 = StopLadder.stage0Candidate(s0, tick, bufferPct);
        assertEquals(1748.30, stage0.triggerPrice(), EPSILON, "Stage 0 trigger must match S0");
        assertEquals(1745.65, stage0.limitPrice(), EPSILON, "Stage 0 limit must be 1745.65");

        // Stage 1 (-0.35R)
        StopLadder.StopOrderPair stage1 = StopLadder.stage1Candidate(entry, s0, tick, bufferPct, 0.35);
        assertEquals(1758.05, stage1.triggerPrice(), EPSILON, "Stage 1 trigger must be 1758.05");
        assertEquals(1755.40, stage1.limitPrice(), EPSILON, "Stage 1 limit must be 1755.40");
    }

    @Test
    @DisplayName("E1 Golden Vectors: TECHM Stage 2 P_floor(₹11) and Trigger Verification")
    void testGoldenVectorsStage2() {
        int qty = 11;
        double entry = 1763.30;
        double tick = 0.05;
        double profitBufferPct = 0.0020; // 0.20%
        double targetNetProfit = 11.00;

        Optional<StopLadder.StopOrderPair> stage2Opt = StopLadder.stage2Candidate(
            qty, entry, tick, profitBufferPct, targetNetProfit
        );
        assertTrue(stage2Opt.isPresent(), "Stage 2 candidate must be present for TECHM q=11");
        StopLadder.StopOrderPair stage2 = stage2Opt.get();

        assertEquals(1766.20, stage2.limitPrice(), EPSILON, "Stage 2 floor limit must be ₹1,766.20");
        assertEquals(1769.75, stage2.triggerPrice(), EPSILON, "Stage 2 trigger must be ₹1,769.75");

        // Verify verified net profit at limit price
        ChargesBreakdown chargesAtLimit = ChargesBreakdown.calculate(qty, entry, stage2.limitPrice());
        assertTrue(chargesAtLimit.netPnL() >= 11.00, "Net profit at floor limit must be >= ₹11.00");
        assertEquals(11.26, chargesAtLimit.netPnL(), 0.02, "Net profit at 1766.20 should be ~₹11.26");
    }

    @Test
    @DisplayName("E1 Golden Vectors: Retention Factor r(g) and Clamping")
    void testRetentionFactorClamping() {
        // g = 0.675% -> unclamped is 0.4458, must clamp to 0.50
        double rLow = StopLadder.retentionFactor(0.675);
        assertEquals(0.50, rLow, EPSILON, "Unclamped r at g=0.675% must clamp to 0.50");

        // g = 1.0% -> exactly 0.50
        assertEquals(0.50, StopLadder.retentionFactor(1.00), EPSILON);

        // g = 1.2760% -> 0.5460
        double rMid = StopLadder.retentionFactor(1.2760);
        assertEquals(0.5460, rMid, 0.0001, "r(1.276%) should be ~0.5460");

        // g = 2.50% -> 0.75
        assertEquals(0.75, StopLadder.retentionFactor(2.50), EPSILON);

        // g = 4.00% -> clamped to 0.75
        assertEquals(0.75, StopLadder.retentionFactor(4.00), EPSILON);
    }

    @Test
    @DisplayName("E1 Golden Vectors: Stage 3 at H = 1785.80")
    void testGoldenVectorsStage3() {
        double entry = 1763.30;
        double s0 = 1748.30;
        double oneR = entry - s0;
        double hwm = entry + 1.5 * oneR; // 1785.80
        double tick = 0.05;
        double profitBufferPct = 0.0020;

        StopLadder.StopOrderPair stage3 = StopLadder.stage3RetentionCandidate(entry, hwm, tick, profitBufferPct);
        assertEquals(1775.60, stage3.limitPrice(), EPSILON, "Stage 3 limit must be ₹1,775.60");
        assertEquals(1779.15, stage3.triggerPrice(), EPSILON, "Stage 3 trigger must be ₹1,779.15");
    }

    @Test
    @DisplayName("E1 Golden Vectors: Monotonic Merge Prevents Regression")
    void testMonotonicMergePreventsRegression() {
        StopLadder.StopOrderPair active = new StopLadder.StopOrderPair(1769.75, 1766.20);
        StopLadder.StopOrderPair candidate = new StopLadder.StopOrderPair(1769.35, 1766.20);

        StopLadder.StopOrderPair merged = active.merge(candidate);
        assertEquals(1769.75, merged.triggerPrice(), EPSILON, "Trigger must not regress");
        assertEquals(1766.20, merged.limitPrice(), EPSILON, "Limit must not regress");
        assertFalse(merged.hasRegressedAgainst(active), "Merged pair must not regress against active");
    }

    @Test
    @DisplayName("E1 Golden Vectors: Sizing at Buy Limit Clamps to 10 Shares and Budget <= ₹200")
    void testSizingAtBuyLimitPrice() {
        double entryLtp = 1763.30;
        double initialStop = 1748.30;
        double tick = 0.05;
        double marketableBuyLimit = TickMath.ceilToTick(entryLtp * 1.0005, tick); // 1764.20

        // Live trade capital ₹10,409.32: Sized at limit gives 10 shares (down from 11 unconstrained)
        int qtyLiveCapital = PositionSizingEngine.calculateAllInclusiveSizingQuantity(
            10409.32, marketableBuyLimit, initialStop, 2.0, 5.0, 0.35, 2.0, 0.15
        );
        assertEquals(10, qtyLiveCapital, "Sizing at buy limit price with live capital 10,409.32 must return 10 shares");

        // Base capital ₹10,000.00: Sized at limit gives 9 shares (or 10 shares at entry LTP)
        int qtyBaseCapital = PositionSizingEngine.calculateAllInclusiveSizingQuantity(
            10000.00, marketableBuyLimit, initialStop, 2.0, 5.0, 0.35, 2.0, 0.15
        );
        assertEquals(9, qtyBaseCapital, "Sizing at buy limit price with base capital ₹10,000 must return 9 shares");

        // Verify all-in loss for 10 shares at L0 (1745.65) is within ₹200 budget for 10,000 capital if entered at LTP
        double l0 = 1745.65;
        ChargesBreakdown charges = ChargesBreakdown.calculate(10, entryLtp, l0);
        assertTrue(Math.abs(charges.netPnL()) <= 200.00, "All-in loss at L0 for 10 shares must be within ₹200 budget");
    }

    @Test
    @DisplayName("E1 Golden Vectors: Small Notional Degrade Path")
    void testSmallNotionalDegradePath() {
        int qty = 1;
        double entry = 250.0;
        double tick = 0.01;

        // Target ₹11 floor must be unattainable within +1.5% for 1 share of ₹250 stock
        Optional<Double> target11Floor = ChargesBreakdown.calculateExactBreakevenPrice(qty, entry, tick, 11.00);
        assertTrue(target11Floor.isEmpty(), "₹11 floor must be unattainable for 1 share of ₹250 stock");

        // Degrade step 1: Cost-only floor (net >= ₹1.00) should succeed at ₹251.28
        Optional<StopLadder.StopOrderPair> degradedStage2 = StopLadder.stage2Candidate(qty, entry, tick);
        assertTrue(degradedStage2.isPresent(), "Cost-only degrade floor should succeed for 1 share of ₹250 stock");
        assertEquals(251.28, degradedStage2.get().limitPrice(), EPSILON, "Cost-only floor limit should be ₹251.28");
        ChargesBreakdown charges = ChargesBreakdown.calculate(qty, entry, degradedStage2.get().limitPrice());
        assertTrue(charges.netPnL() >= 1.00, "Degraded floor net profit must be >= ₹1.00");
    }

    @Test
    @DisplayName("Property Test: 10,000 Random Monotonic Merge Transitions Never Regress")
    void testPropertyRandomMergeNeverRegresses() {
        Random rng = new Random(42);
        StopLadder.StopOrderPair current = new StopLadder.StopOrderPair(100.0, 99.85);

        for (int i = 0; i < 10_000; i++) {
            // Generate random candidate which could be higher, lower, or mixed
            double candTrig = 90.0 + rng.nextDouble() * 30.0;
            double candLim = candTrig - (0.05 + rng.nextDouble() * 0.5);
            StopLadder.StopOrderPair candidate = new StopLadder.StopOrderPair(candTrig, candLim);

            StopLadder.StopOrderPair next = current.merge(candidate);

            assertTrue(next.triggerPrice() >= current.triggerPrice(), "Trigger price regressed");
            assertTrue(next.limitPrice() >= current.limitPrice(), "Limit price regressed");
            assertTrue(next.triggerPrice() >= next.limitPrice(), "Invariant trigger >= limit violated");
            assertFalse(next.hasRegressedAgainst(current), "hasRegressedAgainst reported true");

            current = next;
        }
    }

    @Test
    @DisplayName("Inversion Gap and Executability Gates")
    void testInversionGapAndExecutability() {
        double tick = 0.05;
        double gapAt1770 = StopLadder.calculateInversionGap(1770.0, tick);
        assertEquals(3.55, gapAt1770, EPSILON, "Inversion gap at 1770 must be 3.55");

        double gapAt1758 = StopLadder.calculateInversionGap(1758.80, tick);
        assertEquals(3.55, gapAt1758, EPSILON, "Inversion gap at 1758.80 must be 3.55");

        // Executability: trigger must be <= LTP - gap
        // At LTP = 1770.0, max executable trigger is 1770.0 - 3.55 = 1766.45
        assertTrue(StopLadder.isExecutable(1766.45, 1770.0, tick));
        assertTrue(StopLadder.isExecutable(1760.00, 1770.0, tick));
        assertFalse(StopLadder.isExecutable(1766.50, 1770.0, tick));
        assertFalse(StopLadder.isExecutable(1769.75, 1770.0, tick));
    }

    @Test
    @DisplayName("Emergency Exit Ladder Prices")
    void testEmergencyExitLadder() {
        double bid = 1763.90;
        double tick = 0.05;
        double lowerCircuit = 1600.0;

        // Step 1: floor(bid - max(2*tick, 0.05% * bid)) = floor(1763.90 - 0.88195) = 1763.00
        double step1 = StopLadder.emergencyExitLimit(bid, 1, tick, lowerCircuit);
        assertEquals(1763.00, step1, EPSILON, "Step 1 emergency limit must be 1763.00");

        // Step 2: floor(bid * (1 - 0.25%)) = floor(1763.90 * 0.9975 = 1759.49025) = 1759.45
        double step2 = StopLadder.emergencyExitLimit(bid, 2, tick, lowerCircuit);
        assertEquals(1759.45, step2, EPSILON, "Step 2 emergency limit must be 1759.45");

        // Clamp to lower circuit
        double clamped = StopLadder.emergencyExitLimit(bid, 1, tick, 1765.00);
        assertEquals(1765.00, clamped, EPSILON, "Must clamp to lower circuit limit");
    }
}
