package com.algotrade.algotradebackend.broker;

import com.algotrade.algotradebackend.core.model.ExecutionMode;
import com.algotrade.algotradebackend.core.model.Position;
import com.algotrade.algotradebackend.core.model.TradeOrder;

import java.util.Optional;

/**
 * Universal Broker Gateway interface.
 * 
 * <p>Decouples trading strategy logic from execution venues.
 * Both the simulated environment (PaperTradingGateway) and production broker
 * (ZerodhaLiveGateway) implement this exact contract, allowing hot-swapping
 * between paper trading and real capital without changing strategy code.</p>
 */
public interface BrokerGateway {

    /**
     * Submits a new order to the broker or paper simulation engine.
     * 
     * @param symbol Stock scrip symbol (e.g. "TATAMOTORS")
     * @param transactionType "BUY" or "SELL"
     * @param quantity Number of shares
     * @param price Limit price
     * @param triggerPrice Trigger price for Stop-Loss orders (0.0 for regular limit)
     * @param orderType "LIMIT" or "SL"
     * @return Resulting TradeOrder instance
     */
    TradeOrder placeOrder(String symbol, String transactionType, int quantity, double price, double triggerPrice, String orderType);

    /**
     * In-place atomic modification of an existing resting order.
     * Crucial for ratcheting trailing stop-losses without creating an unhedged gap.
     * 
     * @param orderId Identifier of the open order
     * @param newPrice New limit price
     * @param newTriggerPrice New trigger price
     * @return Updated TradeOrder instance
     */
    TradeOrder modifyOrder(String orderId, double newPrice, double newTriggerPrice);

    /**
     * In-place atomic modification of an existing resting order with quantity expansion.
     * Crucial for partial fill resizing and ratcheting stops without creating an unhedged gap.
     * 
     * @param orderId Identifier of the open order
     * @param newQuantity New total order quantity (if > 0)
     * @param newPrice New limit price
     * @param newTriggerPrice New trigger price
     * @return Updated TradeOrder instance
     */
    default TradeOrder modifyOrder(String orderId, int newQuantity, double newPrice, double newTriggerPrice) {
        return modifyOrder(orderId, newPrice, newTriggerPrice);
    }

    /**
     * Cancels an open resting order.
     * 
     * @param orderId Identifier of the order to cancel
     * @return Cancelled TradeOrder instance
     */
    TradeOrder cancelOrder(String orderId);

    /**
     * Retrieves the status of a specific order by ID, if available.
     * 
     * @param orderId Identifier of the order
     * @return Current TradeOrder details or null if unknown
     */
    default TradeOrder getOrderStatus(String orderId) {
        return null;
    }

    /**
     * Retrieves the currently held open position, if any.
     */
    Optional<Position> getOpenPosition();

    /**
     * Queries available free cash/margin in INR.
     */
    double getAvailableCash();

    /**
     * Dynamically calculates margin required for a proposed order.
     * For Zerodha, calls /margins/orders; for Paper, models 20% margin (5x leverage).
     */
    double calculateOrderMargin(String symbol, int quantity, double price);

    /**
     * Identifies the execution profile of this gateway (PAPER vs LIVE).
     */
    ExecutionMode getExecutionMode();

    /**
     * Retrieves comprehensive stock quote and market depth data.
     */
    java.util.Map<String, Object> getStockQuote(String symbol);

    /**
     * Total account capital / equity in INR including settled realized profits and losses.
     */
    default double getTotalCapital() {
        return getAvailableCash();
    }

    /**
     * Baseline configured seed capital in INR (e.g. ₹10,000.00).
     */
    default double getBaseCapital() {
        return 10000.00;
    }

    /**
     * Cumulative realized net profit/loss in INR after all statutory charges and brokerages.
     */
    default double getCumulativeNetPnL() {
        return Math.round((getTotalCapital() - getBaseCapital()) * 100.0) / 100.0;
    }
}
