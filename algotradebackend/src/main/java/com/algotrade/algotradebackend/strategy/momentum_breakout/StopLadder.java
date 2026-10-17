package com.algotrade.algotradebackend.strategy.momentum_breakout;

import com.algotrade.algotradebackend.core.model.ChargesBreakdown;
import com.algotrade.algotradebackend.core.util.TickMath;
import java.util.Optional;

/**
 * Pure, stateless mathematical engine for multi-stage intraday stop ladder calculations.
 * Implements candidate pair generation, retention curves, monotonic merging,
 * executability gating, and emergency exit ladders per the approved production specification.
 */
public final class StopLadder {

    private StopLadder() {}

    /**
     * Immutable stop order pair (trigger price and limit price) satisfying trigger >= limit.
     */
    public record StopOrderPair(double triggerPrice, double limitPrice) {
        public StopOrderPair {
            if (triggerPrice < limitPrice && triggerPrice > 0.0) {
                throw new IllegalArgumentException(
                    "Invariant violation: triggerPrice (" + triggerPrice + ") must be >= limitPrice (" + limitPrice + ")"
                );
            }
        }

        /**
         * Component-wise maximum merge with another candidate pair.
         * Guarantees monotonic progression: neither trigger nor limit will ever regress.
         */
        public StopOrderPair merge(StopOrderPair candidate) {
            if (candidate == null) return this;
            double newTrigger = Math.max(this.triggerPrice, candidate.triggerPrice);
            double newLimit = Math.max(this.limitPrice, candidate.limitPrice);
            return new StopOrderPair(newTrigger, newLimit);
        }

        /**
         * Checks if the proposed pair regresses against this pair.
         */
        public boolean hasRegressedAgainst(StopOrderPair other) {
            if (other == null) return false;
            return this.triggerPrice < other.triggerPrice || this.limitPrice < other.limitPrice;
        }
    }

    /**
     * Stage 0 Candidate: Trigger-anchored at initial structural stop S0.
     * Limit = floorToTick(S0 * (1 - bufferPct), tickSize).
     */
    public static StopOrderPair stage0Candidate(double s0, double tickSize, double bufferPct) {
        double trigger = s0;
        double limit = TickMath.floorToTick(s0 * (1.0 - bufferPct), tickSize);
        return new StopOrderPair(trigger, limit);
    }

    /**
     * Stage 1 Candidate: Trigger-anchored at floor(E - rMultiplier * 1R).
     * By default rMultiplier = 0.35 (-0.35R soft stop).
     * Limit = floorToTick(trigger * (1 - bufferPct), tickSize).
     */
    public static StopOrderPair stage1Candidate(double entryPrice, double s0, double tickSize, double bufferPct, double rMultiplier) {
        double oneR = entryPrice - s0;
        double rawTrigger = entryPrice - (rMultiplier * oneR);
        double trigger = TickMath.floorToTick(rawTrigger, tickSize);
        double limit = TickMath.floorToTick(trigger * (1.0 - bufferPct), tickSize);
        return new StopOrderPair(trigger, limit);
    }

    public static StopOrderPair stage1Candidate(double entryPrice, double s0, double tickSize, double bufferPct) {
        return stage1Candidate(entryPrice, s0, tickSize, bufferPct, 0.35);
    }

    /**
     * Computes the retention factor r(g) for Stage 3 trailing stops:
     * r(g) = 0.50 + 0.25 * clamp((g - 1.0) / 1.5, 0.0, 1.0)
     * where g is the peak gain in percentage points: (H - E) / E * 100.
     */
    public static double retentionFactor(double peakGainPct) {
        double raw = (peakGainPct - 1.0) / 1.5;
        double clamped = Math.max(0.0, Math.min(1.0, raw));
        return 0.50 + (0.25 * clamped);
    }

    /**
     * Stage 2 Candidate: Limit-anchored at modeled cost floor P_floor(targetNetProfit).
     * Trigger = ceilToTick(floor * (1 + profitBufferPct), tickSize).
     * Implements Stage 2 degrade:
     * 1. Try targetNetProfit (default ₹11.00)
     * 2. If unattainable, try cost-only target (₹1.00)
     * 3. If unattainable, returns Optional.empty() (skip Stage 2).
     */
    public static Optional<StopOrderPair> stage2Candidate(
        int quantity,
        double entryPrice,
        double tickSize,
        double profitBufferPct,
        double targetNetProfit
    ) {
        // Step 1: Attempt target net profit (e.g. ₹11)
        Optional<Double> floorOpt = ChargesBreakdown.calculateExactBreakevenPrice(quantity, entryPrice, tickSize, targetNetProfit);
        if (floorOpt.isEmpty()) {
            // Degrade 1: Retry with cost-only target (net >= ₹1.00)
            floorOpt = ChargesBreakdown.calculateExactBreakevenPrice(quantity, entryPrice, tickSize, 1.00);
        }
        if (floorOpt.isEmpty()) {
            // Degrade 2: Stage 2 cannot be armed; skip
            return Optional.empty();
        }

        double floor = floorOpt.get();
        double trigger = TickMath.ceilToTick(floor * (1.0 + profitBufferPct), tickSize);
        return Optional.of(new StopOrderPair(trigger, floor));
    }

    public static Optional<StopOrderPair> stage2Candidate(int quantity, double entryPrice, double tickSize) {
        return stage2Candidate(quantity, entryPrice, tickSize, 0.0020, 11.00);
    }

    /**
     * Stage 3 Retention Candidate: Limit-anchored at ceilToTick(E + r(g) * (H - E), tickSize).
     * Trigger = ceilToTick(rawFloor * (1 + profitBufferPct), tickSize).
     */
    public static StopOrderPair stage3RetentionCandidate(
        double entryPrice,
        double highWaterMark,
        double tickSize,
        double profitBufferPct
    ) {
        double g = (highWaterMark - entryPrice) / entryPrice * 100.0;
        double r = retentionFactor(g);
        double rawFloor = entryPrice + r * (highWaterMark - entryPrice);
        double limit = TickMath.ceilToTick(rawFloor, tickSize);
        double trigger = TickMath.ceilToTick(rawFloor * (1.0 + profitBufferPct), tickSize);
        trigger = Math.max(trigger, limit);
        return new StopOrderPair(trigger, limit);
    }

    /**
     * Stage 3 Structural Candidate: Trigger-anchored at floorToTick(min(low1, low2) - tickSize, tickSize).
     * Limit = floorToTick(trigger * (1 - bufferPct), tickSize).
     */
    public static Optional<StopOrderPair> stage3StructuralCandidate(
        double low1,
        double low2,
        double tickSize,
        double bufferPct
    ) {
        if (low1 <= 0.0 || low2 <= 0.0) {
            return Optional.empty();
        }
        double trigger = TickMath.floorToTick(Math.min(low1, low2) - tickSize, tickSize);
        double limit = TickMath.floorToTick(trigger * (1.0 - bufferPct), tickSize);
        return Optional.of(new StopOrderPair(trigger, limit));
    }

    /**
     * Stage 3 Combined Candidate: Max of retention candidate and optional structural candidate.
     */
    public static StopOrderPair stage3Candidate(
        double entryPrice,
        double highWaterMark,
        double tickSize,
        double profitBufferPct,
        double lossBufferPct,
        Optional<Double> low1,
        Optional<Double> low2
    ) {
        StopOrderPair retention = stage3RetentionCandidate(entryPrice, highWaterMark, tickSize, profitBufferPct);
        if (low1 != null && low2 != null && low1.isPresent() && low2.isPresent() && low1.get() > 0.0 && low2.get() > 0.0) {
            Optional<StopOrderPair> structural = stage3StructuralCandidate(low1.get(), low2.get(), tickSize, lossBufferPct);
            if (structural.isPresent()) {
                return retention.merge(structural.get());
            }
        }
        return retention;
    }

    /**
     * Calculates the minimum inversion gap: max(minGapTicks * tickSize, ceilToTick(ltp * gapPct / 100, tickSize)).
     */
    public static double calculateInversionGap(double ltp, double tickSize, int minGapTicks, double gapPct) {
        double minTickGap = minGapTicks * tickSize;
        double pctGap = TickMath.ceilToTick(ltp * (gapPct / 100.0), tickSize);
        return Math.max(minTickGap, pctGap);
    }

    public static double calculateInversionGap(double ltp, double tickSize) {
        return calculateInversionGap(ltp, tickSize, 5, 0.20);
    }

    /**
     * Executability check for opportunistic price ratchets:
     * Requires triggerPrice <= LTP - gap.
     */
    public static boolean isExecutable(double triggerPrice, double ltp, double tickSize, int minGapTicks, double gapPct) {
        double gap = calculateInversionGap(ltp, tickSize, minGapTicks, gapPct);
        return triggerPrice <= (ltp - gap);
    }

    public static boolean isExecutable(double triggerPrice, double ltp, double tickSize) {
        return isExecutable(triggerPrice, ltp, tickSize, 5, 0.20);
    }

    /**
     * Stagnation check:
     * Evaluated once elapsedMinutes >= 25.
     * Stagnant if no increase in HWM of at least 0.25R during last 15 min and logicalStage < 3.
     */
    public static boolean isStagnant(
        long elapsedMinutes,
        double currentHwm,
        double hwm15MinAgo,
        double unitRisk,
        int logicalStage,
        long stagnationThresholdMinutes,
        double stagnationProgressR
    ) {
        if (elapsedMinutes < stagnationThresholdMinutes) return false;
        if (logicalStage >= 3) return false;
        double progress = currentHwm - hwm15MinAgo;
        return progress < (stagnationProgressR * unitRisk);
    }

    public static boolean isStagnant(long elapsedMinutes, double currentHwm, double hwm15MinAgo, double unitRisk, int logicalStage) {
        return isStagnant(elapsedMinutes, currentHwm, hwm15MinAgo, unitRisk, logicalStage, 25, 0.25);
    }

    /**
     * PROTECTED exit limit: max(floor, bestBid), clamped to lowerCircuitLimit.
     */
    public static double protectedExitLimit(double floorPrice, double bestBid, double lowerCircuitLimit) {
        double limit = Math.max(floorPrice, bestBid);
        return Math.max(lowerCircuitLimit, limit);
    }

    /**
     * EMERGENCY exit ladder limit:
     * Step 1 (t = 0): floorToTick(bestBid - max(2 * tickSize, 0.05% * bestBid), tickSize)
     * Step 2 (t + 2s): floorToTick(bestBid * (1 - 0.25%), tickSize)
     * Step 3 (t + 4s): floorToTick(bestBid * (1 - 0.50%), tickSize)
     * Step 4+ (every 3s): floorToTick(bestBid * (1 - 1.00%), tickSize)
     * Clamped to lowerCircuitLimit.
     */
    public static double emergencyExitLimit(double bestBid, int step, double tickSize, double lowerCircuitLimit) {
        double rawLimit;
        if (step <= 1) {
            double tickBuffer = 2.0 * tickSize;
            double pctBuffer = bestBid * 0.0005;
            rawLimit = bestBid - Math.max(tickBuffer, pctBuffer);
        } else if (step == 2) {
            rawLimit = bestBid * (1.0 - 0.0025);
        } else if (step == 3) {
            rawLimit = bestBid * (1.0 - 0.0050);
        } else {
            rawLimit = bestBid * (1.0 - 0.0100);
        }
        double floored = TickMath.floorToTick(rawLimit, tickSize);
        return Math.max(lowerCircuitLimit, floored);
    }
}
