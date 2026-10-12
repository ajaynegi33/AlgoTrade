package com.algotrade.algotradebackend.strategy.momentum_breakout;

/**
 * Immutable, thread-safe point-in-time observation of candidate stock metrics.
 * Captured atomically under lock to avoid partial-update races during scoring.
 */
public record CandidateSnapshot(
    String symbol,
    double ltp,
    double openPrice,
    double low15,
    double high15,
    double prevClose,
    double previousDayHigh,
    double avgDailyVolume10Day,
    double atrPct,
    long windowVolume,
    double vwap,
    double depthImbalance,
    double gapPct
) {}
