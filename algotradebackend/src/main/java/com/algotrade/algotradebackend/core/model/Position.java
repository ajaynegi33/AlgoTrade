package com.algotrade.algotradebackend.core.model;

import java.math.BigDecimal;
import java.math.RoundingMode;
import java.time.Instant;

/**
 * Represents an open intraday trade position held by the algorithmic engine.
 * 
 * <p>Tracks real-time performance, including the High-Water Mark reached since entry,
 * the initial protective stop-loss, and the actively ratcheting trailing stop level.</p>
 */
public record Position(
    String symbol,
    int quantity,
    double entryPrice,
    double currentPrice,
    double initialStopLoss,
    double activeTrailingStop,
    double highWaterMark,
    Instant entryTime,
    ExecutionMode mode
) {

    /**
     * Calculates the gross unrealized profit or loss in INR.
     */
    public double getUnrealizedPnL() {
        return round((currentPrice - entryPrice) * quantity);
    }

    /**
     * Calculates the percentage return from the entry price.
     */
    public double getUnrealizedPnLPct() {
        if (entryPrice <= 0) return 0.0;
        return round(((currentPrice - entryPrice) / entryPrice) * 100.0);
    }

    /**
     * Creates an updated Position when a new market tick arrives.
     * Updates the High-Water Mark if the new price exceeds the previous high.
     * 
     * @param newPrice Latest market price (LTP)
     * @param newTrailingStop Newly calculated trailing stop level (or unchanged)
     * @return Updated Position instance
     */
    public Position withTickUpdate(double newPrice, double newTrailingStop) {
        double updatedHWM = Math.max(this.highWaterMark, newPrice);
        double updatedTrail = Math.max(this.activeTrailingStop, newTrailingStop);
        return new Position(
            symbol,
            quantity,
            entryPrice,
            newPrice,
            initialStopLoss,
            updatedTrail,
            updatedHWM,
            entryTime,
            mode
        );
    }

    private static double round(double val) {
        return BigDecimal.valueOf(val).setScale(2, RoundingMode.HALF_UP).doubleValue();
    }
}
