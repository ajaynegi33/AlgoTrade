package com.algotrade.algotradebackend.backtest.model;

/**
 * Daily equity curve point showing capital progression, daily PnL, and current drawdown.
 */
public record BacktestEquityPoint(
    String date,
    double capital,
    double grossPnL,
    double netPnL,
    double drawdownPct
) {}
