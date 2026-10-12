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
    Double slippagePct
) {
    public BacktestRequest {
        if (initialCapital == null || initialCapital <= 0) initialCapital = 10000.0;
        if (riskPerTradePct == null || riskPerTradePct <= 0) riskPerTradePct = 1.0;
        if (minScoreThreshold == null || minScoreThreshold <= 0) minScoreThreshold = 75;
        if (trailingSlPct == null || trailingSlPct <= 0) trailingSlPct = 2.0;
        if (slippagePct == null || slippagePct < 0) slippagePct = 0.05;
    }
}
