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
    double averagePrice,
    String orderType,       // "LIMIT" or "SL" or "MARKET"
    String status,          // "PENDING", "TRIGGER PENDING", "OPEN", "COMPLETE", "CANCELLED", "REJECTED"
    ExecutionMode mode,
    Instant createdAt,
    String tag,
    int filledQuantity,
    int pendingQuantity,
    String statusMessage
) {
    /**
     * Backward-compatible 11-parameter constructor defaulting tag, filledQuantity, pendingQuantity, and statusMessage.
     */
    public TradeOrder(
        String orderId,
        String symbol,
        String transactionType,
        int quantity,
        double price,
        double triggerPrice,
        double averagePrice,
        String orderType,
        String status,
        ExecutionMode mode,
        Instant createdAt
    ) {
        this(
            orderId, symbol, transactionType, quantity, price, triggerPrice, averagePrice, orderType, status, mode, createdAt,
            null,
            ("COMPLETE".equalsIgnoreCase(status) || "FILLED".equalsIgnoreCase(status)) ? quantity : 0,
            ("COMPLETE".equalsIgnoreCase(status) || "FILLED".equalsIgnoreCase(status)) ? 0 : quantity,
            null
        );
    }

    /**
     * Backward-compatible 10-parameter constructor defaulting averagePrice to price.
     */
    public TradeOrder(
        String orderId,
        String symbol,
        String transactionType,
        int quantity,
        double price,
        double triggerPrice,
        String orderType,
        String status,
        ExecutionMode mode,
        Instant createdAt
    ) {
        this(orderId, symbol, transactionType, quantity, price, triggerPrice, price, orderType, status, mode, createdAt);
    }

    /**
     * Helper to create a new pending order before submission.
     */
    public static TradeOrder create(String orderId, String symbol, String transactionType, int quantity, double price, double triggerPrice, String orderType, ExecutionMode mode) {
        return new TradeOrder(orderId, symbol, transactionType, quantity, price, triggerPrice, price, orderType, "PENDING", mode, Instant.now());
    }

    /**
     * Returns a copy with updated execution status.
     */
    public TradeOrder withStatus(String newStatus) {
        int filled = ("COMPLETE".equalsIgnoreCase(newStatus) || "FILLED".equalsIgnoreCase(newStatus)) ? quantity : filledQuantity;
        int pending = ("COMPLETE".equalsIgnoreCase(newStatus) || "FILLED".equalsIgnoreCase(newStatus)) ? 0 : (quantity - filled);
        return new TradeOrder(orderId, symbol, transactionType, quantity, price, triggerPrice, averagePrice, orderType, newStatus, mode, createdAt, tag, filled, pending, statusMessage);
    }

    /**
     * Returns a copy with updated execution status and average executed fill price.
     */
    public TradeOrder withExecution(String newStatus, double executedAveragePrice) {
        int filled = ("COMPLETE".equalsIgnoreCase(newStatus) || "FILLED".equalsIgnoreCase(newStatus)) ? quantity : filledQuantity;
        int pending = ("COMPLETE".equalsIgnoreCase(newStatus) || "FILLED".equalsIgnoreCase(newStatus)) ? 0 : (quantity - filled);
        return new TradeOrder(orderId, symbol, transactionType, quantity, price, triggerPrice, executedAveragePrice, orderType, newStatus, mode, createdAt, tag, filled, pending, statusMessage);
    }

    public TradeOrder withTag(String newTag) {
        return new TradeOrder(orderId, symbol, transactionType, quantity, price, triggerPrice, averagePrice, orderType, status, mode, createdAt, newTag, filledQuantity, pendingQuantity, statusMessage);
    }
}
