package com.algotrade.algotradebackend.backtest.model;

import java.util.List;

/**
 * Complete response payload for a backtest simulation run.
 */
public record BacktestResult(
    BacktestSummary summary,
    List<BacktestEquityPoint> equityCurve,
    List<BacktestTrade> trades
) {}
