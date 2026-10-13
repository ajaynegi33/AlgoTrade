package com.algotrade.algotradebackend.backtest.model;

/**
 * Request payload for configuring and launching a backtest simulation run.
 */
public record BacktestRequest(
    String startDate,
    String endDate,
    Double initialCapital,
    Double riskPerTradePct,
    Integer minScoreThreshold,
    Double trailingSlPct,
    Double slippagePct,
    Double leverageMultiplier
) {
    public BacktestRequest {
        if (initialCapital == null || initialCapital <= 0) initialCapital = 10000.0;
        if (riskPerTradePct == null || riskPerTradePct <= 0) riskPerTradePct = 1.0;
        if (minScoreThreshold == null || minScoreThreshold <= 0) minScoreThreshold = 75;
        if (trailingSlPct == null || trailingSlPct <= 0) trailingSlPct = 0.8;
        if (slippagePct == null || slippagePct < 0) slippagePct = 0.05;
        if (leverageMultiplier == null || leverageMultiplier < 1.0) leverageMultiplier = 1.0;
        if (leverageMultiplier > 5.0) leverageMultiplier = 5.0; // Clamped to SEBI 5x MIS regulatory limit
    }

    public BacktestRequest(
        String startDate,
        String endDate,
        Double initialCapital,
        Double riskPerTradePct,
        Integer minScoreThreshold,
        Double trailingSlPct,
        Double slippagePct
    ) {
        this(startDate, endDate, initialCapital, riskPerTradePct, minScoreThreshold, trailingSlPct, slippagePct, 1.0);
    }
}
