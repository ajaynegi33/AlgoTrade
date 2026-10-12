package com.algotrade.algotradebackend.backtest.model;

/**
 * Detailed audit record of a single trade executed during a backtest simulation.
 */
public record BacktestTrade(
    int id,
    String date,
    String symbol,
    int score,
    double entryPrice,
    double exitPrice,
    int quantity,
    String entryTime,
    String exitTime,
    String exitReason,
    double grossPnL,
    double totalCharges,
    double netPnL,
    double returnPct
) {}
