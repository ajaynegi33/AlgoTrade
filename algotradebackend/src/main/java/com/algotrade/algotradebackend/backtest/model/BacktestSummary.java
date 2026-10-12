package com.algotrade.algotradebackend.backtest.model;

/**
 * High-level institutional quantitative performance and risk metrics summary.
 */
public record BacktestSummary(
    double initialCapital,
    double finalCapital,
    double totalGrossPnL,
    double totalTaxes,
    double totalNetPnL,
    double totalReturnPct,
    int totalTrades,
    int winTrades,
    int lossTrades,
    double winRatePct,
    double profitFactor,
    double maxDrawdownPct,
    double maxDrawdownRupees,
    int maxConsecutiveLosses,
    double averageTradeNetPnL,
    double sharpeRatio
) {}
