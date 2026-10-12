package com.algotrade.algotradebackend.core.model;

import java.time.Instant;

/**
 * Standardized order representation used across both Paper Trading and Live Brokerage.
 * 
 * <p>Encapsulates all order metadata: target symbol, transaction side (BUY/SELL),
 * quantity, limit price, trigger price, and current lifecycle state.</p>
 */
public record TradeOrder(
    String orderId,
    String symbol,
    String transactionType, // "BUY" or "SELL"
    int quantity,
    double price,
    double triggerPrice,
    String orderType,       // "LIMIT" or "SL"
    String status,          // "PENDING", "FILLED", "CANCELLED", "REJECTED"
    ExecutionMode mode,
    Instant createdAt
) {
    /**
     * Helper to create a new pending order before submission.
     */
    public static TradeOrder create(String orderId, String symbol, String transactionType, int quantity, double price, double triggerPrice, String orderType, ExecutionMode mode) {
        return new TradeOrder(orderId, symbol, transactionType, quantity, price, triggerPrice, orderType, "PENDING", mode, Instant.now());
    }

    /**
     * Returns a copy with updated execution status.
     */
    public TradeOrder withStatus(String newStatus) {
        return new TradeOrder(orderId, symbol, transactionType, quantity, price, triggerPrice, orderType, newStatus, mode, createdAt);
    }
}
