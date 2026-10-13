package com.algotrade.algotradebackend.strategy;

import com.algotrade.algotradebackend.core.model.ScoredCandidate;

import java.util.List;

/**
 * Common Strategy Pattern interface for all trading strategies.
 * 
 * <p>Each algorithmic strategy is implemented in its own isolated feature folder
 * (e.g. `strategy/momentum_breakout/`). This interface guarantees that any future
 * strategy (such as VWAP Reversion or Pairs Trading) can be registered and hot-swapped
 * by the StrategyManager without altering the core execution engine.</p>
 */
public interface TradingStrategy {

    /**
     * Unique machine identifier for this strategy (e.g. "MOMENTUM_BREAKOUT").
     */
    String getStrategyId();

    /**
     * Human-readable display name for the frontend dashboard.
     */
    String getStrategyName();

    /**
     * Ingests real-time tick updates streamed from the active broker or simulator.
     * 
     * @param symbol Stock scrip symbol
     * @param ltp Last Traded Price
     * @param volume Cumulative Volume Traded Today (VTT)
     * @param totalBuyQty Level 2 Total Bid Quantity
     * @param totalSellQty Level 2 Total Ask Quantity
     */
    void onTick(String symbol, double ltp, long volume, long totalBuyQty, long totalSellQty);

    /**
     * Triggered at 09:30:00 AM IST to run candidate scoring, select the top stock,
     * and dispatch the initial entry limit order.
     */
    void evaluate0930();

    /**
     * Runs the continuous all-day intraday evaluation cycle across the universe.
     */
    default void evaluateContinuousCycle() {
        evaluate0930();
    }

    /**
     * Exposes the set of symbols already traded today.
     */
    default java.util.Set<String> getTradedSymbolsToday() {
        return java.util.Collections.emptySet();
    }

    /**
     * Triggered at 15:05:00 PM IST for mandatory intraday square-off ahead of broker CAS cutoffs.
     */
    void executeSquareOff();

    /**
     * Backwards-compatible alias for executeSquareOff.
     */
    default void executeSquareOff1515() {
        executeSquareOff();
    }

    /**
     * Exposes the latest evaluated Top 10 stocks and their score breakdowns
     * to be displayed on the Next.js frontend leaderboard.
     */
    List<ScoredCandidate> getLatestTopCandidates();

    /**
     * Executes a manual discretionary entry and hands over trade management to the strategy.
     */
    default java.util.Map<String, Object> executeManualEntry(String symbol, Integer quantity, Double customStopPrice) {
        return executeManualEntry(symbol, quantity, customStopPrice, null);
    }

    default java.util.Map<String, Object> executeManualEntry(String symbol, Integer quantity, Double customStopPrice, Double entryPrice) {
        throw new UnsupportedOperationException("Manual entry is not supported by " + getStrategyId());
    }
}
