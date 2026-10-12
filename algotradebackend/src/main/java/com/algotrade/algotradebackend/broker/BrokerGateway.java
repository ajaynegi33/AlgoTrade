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
     * Cancels an open resting order.
     * 
     * @param orderId Identifier of the order to cancel
     * @return Cancelled TradeOrder instance
     */
    TradeOrder cancelOrder(String orderId);

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
}
