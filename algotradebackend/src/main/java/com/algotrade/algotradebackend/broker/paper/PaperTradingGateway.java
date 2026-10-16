package com.algotrade.algotradebackend.broker.paper;

import com.algotrade.algotradebackend.broker.BrokerGateway;
import com.algotrade.algotradebackend.core.model.*;
import com.algotrade.algotradebackend.persistence.entity.PaperActivePositionEntity;
import com.algotrade.algotradebackend.persistence.entity.PaperTradeEntity;
import com.algotrade.algotradebackend.persistence.repository.PaperActivePositionRepository;
import com.algotrade.algotradebackend.persistence.repository.PaperTradeRepository;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;

import java.time.Instant;
import java.util.Optional;
import java.util.UUID;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicReference;

/**
 * Paper Trading Execution Engine.
 * 
 * <p>Simulates real-world exchange execution with:</p>
 * <ul>
 *   <li><b>Realistic Slippage:</b> Applies 0.05% simulated execution slippage against the trader.</li>
 *   <li><b>Statutory Tax Accounting:</b> Uses ChargesBreakdown to deduct Brokerage, STT, Exchange,
 *       SEBI, Stamp Duty, and GST from the virtual account.</li>
 *   <li><b>Isolated SQLite Persistence:</b> Saves closed trades to `algotradepaper.db` so paper history
 *       is completely segregated from live Zerodha history.</li>
 *   <li><b>Restart Resiliency:</b> Persists active open positions to `paper_active_positions` across server reboots.</li>
 * </ul>
 */
@Service
public class PaperTradingGateway implements BrokerGateway {

    private static final Logger log = LoggerFactory.getLogger(PaperTradingGateway.class);

    private final PaperTradeRepository paperTradeRepository;
    private final PaperActivePositionRepository paperActivePositionRepository;
    private final AtomicReference<Position> openPosition = new AtomicReference<>(null);
    private final ConcurrentHashMap<String, TradeOrder> orders = new ConcurrentHashMap<>();
    private final AtomicReference<Double> virtualCash;
    private final double baseCapital;

    @org.springframework.beans.factory.annotation.Autowired(required = false)
    private org.springframework.beans.factory.ObjectProvider<com.algotrade.algotradebackend.broker.zerodha.ZerodhaLiveGateway> zerodhaLiveGatewayProvider;

    @org.springframework.beans.factory.annotation.Autowired(required = false)
    private org.springframework.beans.factory.ObjectProvider<com.algotrade.algotradebackend.websocket.LiveWebSocketBroadcastService> broadcastServiceProvider;

    public PaperTradingGateway(
        PaperTradeRepository paperTradeRepository,
        @Value("${algotrade.risk.capital:10000.00}") double initialCapital
    ) {
        this(paperTradeRepository, null, initialCapital);
    }

    @org.springframework.beans.factory.annotation.Autowired
    public PaperTradingGateway(
        PaperTradeRepository paperTradeRepository,
        @org.springframework.beans.factory.annotation.Autowired(required = false) PaperActivePositionRepository paperActivePositionRepository,
        @Value("${algotrade.risk.capital:10000.00}") double initialCapital
    ) {
        this.paperTradeRepository = paperTradeRepository;
        this.paperActivePositionRepository = paperActivePositionRepository;
        this.baseCapital = initialCapital;
        double persistedNetPnL = 0.0;
        try {
            var trades = paperTradeRepository.findAll();
            if (trades != null && !trades.isEmpty()) {
                persistedNetPnL = trades.stream()
                    .filter(t -> t.getNetPnL() != null)
                    .mapToDouble(PaperTradeEntity::getNetPnL)
                    .sum();
            }
        } catch (Throwable t) {
            log.warn("Could not load persisted paper trades for balance reconstruction: {}", t.getMessage());
        }
        double startingCash = Math.round((initialCapital + persistedNetPnL) * 100.0) / 100.0;
        this.virtualCash = new AtomicReference<>(startingCash);
        log.info("Initialized PaperTradingGateway: Base Capital=₹{}, Cumulative Settled PnL=₹{}, Current Net Equity=₹{}",
            initialCapital, Math.round(persistedNetPnL * 100.0) / 100.0, startingCash);

        // Re-hydrate active position if one exists in database across restart
        restoreActivePositionOnStartup();
    }

    private void restoreActivePositionOnStartup() {
        if (paperActivePositionRepository == null) return;
        try {
            var opt = paperActivePositionRepository.findFirstByOrderByEntryTimeDesc();
            if (opt.isPresent()) {
                PaperActivePositionEntity entity = opt.get();
                Position restoredPos = new Position(
                    entity.getSymbol(),
                    entity.getQuantity(),
                    entity.getEntryPrice(),
                    entity.getCurrentPrice(),
                    entity.getInitialStopLoss(),
                    entity.getActiveTrailingStop(),
                    entity.getHighWaterMark(),
                    entity.getEntryTime(),
                    ExecutionMode.PAPER
                );
                this.openPosition.set(restoredPos);
                if (entity.getActiveSlOrderId() != null && entity.getActiveTrailingStop() > 0) {
                    double triggerPrice = entity.getActiveTrailingStop();
                    double limitPrice = Math.round(triggerPrice * 0.9985 * 100.0) / 100.0;
                    TradeOrder slOrder = new TradeOrder(
                        entity.getActiveSlOrderId(),
                        entity.getSymbol(),
                        "SELL",
                        entity.getQuantity(),
                        limitPrice,
                        triggerPrice,
                        "SL",
                        "TRIGGER PENDING",
                        ExecutionMode.PAPER,
                        entity.getEntryTime()
                    );
                    this.orders.put(slOrder.orderId(), slOrder);
                }
                log.info("RESTORED PERSISTED PAPER POSITION: {} x {} @ ₹{} (SL: ₹{}, HWM: ₹{})",
                    entity.getQuantity(), entity.getSymbol(), entity.getEntryPrice(),
                    entity.getActiveTrailingStop(), entity.getHighWaterMark());
            }
        } catch (Throwable t) {
            log.warn("Could not restore active paper position: {}", t.getMessage());
        }
    }

    @Override
    public synchronized TradeOrder placeOrder(String symbol, String transactionType, int quantity, double price, double triggerPrice, String orderType) {
        String orderId = "PAPER-" + UUID.randomUUID().toString().substring(0, 8).toUpperCase();

        if ("BUY".equalsIgnoreCase(transactionType) && ("LIMIT".equalsIgnoreCase(orderType) || "MARKET".equalsIgnoreCase(orderType))) {
            // Realistic entry execution: If LIMIT order with price specified, fills at the limit price (no secondary slippage penalty)
            Position existing = openPosition.get();
            double executedPrice;
            if ("LIMIT".equalsIgnoreCase(orderType) && price > 0.0) {
                executedPrice = price; // Strategy already factored in marketable limit price
            } else {
                double basePrice = price > 0.0 ? price : (existing != null ? existing.currentPrice() : 0.0);
                executedPrice = Math.round((basePrice * 1.0005) * 100.0) / 100.0;
            }
            TradeOrder filledOrder = new TradeOrder(orderId, symbol, "BUY", quantity, executedPrice, 0.0, orderType.toUpperCase(), "FILLED", ExecutionMode.PAPER, Instant.now());
            orders.put(orderId, filledOrder);

            // Create initial position
            Position pos = new Position(symbol, quantity, executedPrice, executedPrice, 0.0, 0.0, executedPrice, Instant.now(), ExecutionMode.PAPER);
            openPosition.set(pos);
            if (paperActivePositionRepository != null) {
                try {
                    paperActivePositionRepository.deleteAll();
                    paperActivePositionRepository.save(new PaperActivePositionEntity(
                        symbol, quantity, executedPrice, executedPrice, 0.0, 0.0, executedPrice, pos.entryTime(), null
                    ));
                } catch (Throwable t) {
                    log.warn("Failed to persist open paper position: {}", t.getMessage());
                }
            }
            log.info("PAPER TRADE FILLED: BUY {} x {} @ ₹{}", quantity, symbol, executedPrice);
            return filledOrder;
        }

        if ("SELL".equalsIgnoreCase(transactionType) && "SL".equalsIgnoreCase(orderType)) {
            // Resting Stop-Loss order
            TradeOrder slOrder = new TradeOrder(orderId, symbol, "SELL", quantity, price, triggerPrice, "SL", "TRIGGER PENDING", ExecutionMode.PAPER, Instant.now());
            orders.put(orderId, slOrder);

            // Update position with initial stop-loss
            Position current = openPosition.get();
            if (current != null && current.symbol().equals(symbol)) {
                openPosition.set(new Position(
                    current.symbol(), current.quantity(), current.entryPrice(), current.currentPrice(),
                    triggerPrice, triggerPrice, current.highWaterMark(), current.entryTime(), ExecutionMode.PAPER
                ));
                if (paperActivePositionRepository != null) {
                    try {
                        var opt = paperActivePositionRepository.findFirstByOrderByEntryTimeDesc();
                        if (opt.isPresent()) {
                            PaperActivePositionEntity e = opt.get();
                            e.setInitialStopLoss(triggerPrice);
                            e.setActiveTrailingStop(triggerPrice);
                            e.setActiveSlOrderId(orderId);
                            paperActivePositionRepository.save(e);
                        }
                    } catch (Throwable t) {
                        log.warn("Failed to update active paper position stop in DB: {}", t.getMessage());
                    }
                }
            }
            log.info("PAPER SL PLACED: {} Trigger @ ₹{}, Limit @ ₹{}", symbol, triggerPrice, price);
            return slOrder;
        }

        if ("SELL".equalsIgnoreCase(transactionType) && ("LIMIT".equalsIgnoreCase(orderType) || "MARKET".equalsIgnoreCase(orderType))) {
            // Direct aggressive exit or market square-off
            Position current = openPosition.get();
            double basePrice = price > 0.0 ? price : (current != null ? current.currentPrice() : 0.0);
            double executedPrice = Math.round((basePrice * 0.9995) * 100.0) / 100.0;
            TradeOrder exitOrder = new TradeOrder(orderId, symbol, "SELL", quantity, executedPrice, 0.0, orderType.toUpperCase(), "FILLED", ExecutionMode.PAPER, Instant.now());
            orders.put(orderId, exitOrder);
            closePosition(symbol, executedPrice, "MANUAL_OR_TIME_EXIT");
            return exitOrder;
        }

        TradeOrder pendingOrder = new TradeOrder(orderId, symbol, transactionType, quantity, price, triggerPrice, orderType, "PENDING", ExecutionMode.PAPER, Instant.now());
        orders.put(orderId, pendingOrder);
        return pendingOrder;
    }

    @Override
    public synchronized TradeOrder modifyOrder(String orderId, double newPrice, double newTriggerPrice) {
        return modifyOrder(orderId, 0, newPrice, newTriggerPrice);
    }

    @Override
    public synchronized TradeOrder modifyOrder(String orderId, int newQuantity, double newPrice, double newTriggerPrice) {
        TradeOrder existing = orders.get(orderId);
        if (existing == null) {
            throw new IllegalArgumentException("Paper order not found: " + orderId);
        }

        int finalQty = newQuantity > 0 ? newQuantity : existing.quantity();
        TradeOrder modified = new TradeOrder(
            existing.orderId(), existing.symbol(), existing.transactionType(),
            finalQty, newPrice, newTriggerPrice, existing.orderType(),
            existing.status(), ExecutionMode.PAPER, existing.createdAt()
        );
        orders.put(orderId, modified);

        // Update position's trailing stop and quantity if expanded
        Position current = openPosition.get();
        if (current != null && current.symbol().equals(existing.symbol())) {
            int posQty = newQuantity > 0 ? Math.max(current.quantity(), newQuantity) : current.quantity();
            openPosition.set(new Position(
                current.symbol(), posQty, current.entryPrice(), current.currentPrice(),
                current.initialStopLoss(), newTriggerPrice, current.highWaterMark(), current.entryTime(), ExecutionMode.PAPER
            ));
            if (paperActivePositionRepository != null) {
                try {
                    var opt = paperActivePositionRepository.findFirstByOrderByEntryTimeDesc();
                    if (opt.isPresent()) {
                        PaperActivePositionEntity e = opt.get();
                        e.setActiveTrailingStop(newTriggerPrice);
                        if (newQuantity > 0) e.setQuantity(posQty);
                        paperActivePositionRepository.save(e);
                    }
                } catch (Throwable t) {
                    log.warn("Failed to update trailing stop in DB: {}", t.getMessage());
                }
            }
        }

        log.info("PAPER SL MODIFIED: {} Qty={}, New Trigger @ ₹{}", existing.symbol(), finalQty, newTriggerPrice);
        return modified;
    }

    @Override
    public synchronized TradeOrder cancelOrder(String orderId) {
        TradeOrder existing = orders.get(orderId);
        if (existing != null) {
            TradeOrder cancelled = existing.withStatus("CANCELLED");
            orders.put(orderId, cancelled);
            return cancelled;
        }
        return null;
    }

    /**
     * Simulated price tick processor.
     * Evaluates resting stop orders and ratchets trailing stop levels.
     */
    public synchronized void onTick(String symbol, double ltp) {
        Position current = openPosition.get();
        if (current == null || !current.symbol().equals(symbol)) {
            return;
        }

        // Update current price and High-Water Mark
        double newHWM = Math.max(current.highWaterMark(), ltp);
        openPosition.set(new Position(
            current.symbol(), current.quantity(), current.entryPrice(), ltp,
            current.initialStopLoss(), current.activeTrailingStop(), newHWM, current.entryTime(), ExecutionMode.PAPER
        ));

        // When a new peak is reached, persist to database so restarts don't revert HWM
        if (newHWM > current.highWaterMark() && paperActivePositionRepository != null) {
            try {
                var opt = paperActivePositionRepository.findFirstByOrderByEntryTimeDesc();
                if (opt.isPresent()) {
                    PaperActivePositionEntity e = opt.get();
                    e.setHighWaterMark(newHWM);
                    e.setCurrentPrice(ltp);
                    paperActivePositionRepository.save(e);
                }
            } catch (Throwable ignored) {}
        }

        // Check if Stop-Loss trigger was breached (0.15% slippage to match live SL-Limit offset)
        if (current.activeTrailingStop() > 0 && ltp <= current.activeTrailingStop()) {
            double exitPrice = Math.round((current.activeTrailingStop() * 0.9985) * 100.0) / 100.0;
            log.warn("PAPER STOP HIT: {} Triggered @ ₹{}, Exited @ ₹{}", symbol, current.activeTrailingStop(), exitPrice);
            closePosition(symbol, exitPrice, "STOP_LOSS_HIT");
        }
    }

    /**
     * Closes the active position, computes Indian equity taxes, and persists to SQLite.
     */
    private void closePosition(String symbol, double exitPrice, String exitReason) {
        Position pos = openPosition.getAndSet(null);
        if (pos == null) return;

        // Clean up persisted active position from SQLite
        if (paperActivePositionRepository != null) {
            try {
                paperActivePositionRepository.deleteAll();
            } catch (Throwable t) {
                log.warn("Failed to clear active paper position from database: {}", t.getMessage());
            }
        }

        // Calculate exact statutory tax breakdown
        ChargesBreakdown charges = ChargesBreakdown.calculate(pos.quantity(), pos.entryPrice(), exitPrice);

        // Update virtual ledger balance
        virtualCash.updateAndGet(cash -> cash + charges.netPnL());

        // Persist to isolated SQLite database
        PaperTradeEntity entity = new PaperTradeEntity(
            symbol,
            pos.quantity(),
            pos.entryPrice(),
            exitPrice,
            pos.entryTime(),
            Instant.now(),
            exitReason,
            charges.grossPnL(),
            charges.netPnL(),
            charges.totalCharges(),
            charges.brokerage(),
            charges.stt(),
            charges.exchangeCharges(),
            charges.sebiCharges(),
            charges.stampDuty(),
            charges.gst()
        );
        paperTradeRepository.save(entity);

        // Update resting SL order in orders map to FILLED
        orders.values().stream()
            .filter(o -> o.symbol().equals(symbol) && "TRIGGER PENDING".equals(o.status()))
            .findFirst()
            .ifPresent(o -> orders.put(o.orderId(), o.withStatus("FILLED")));

        log.info("PAPER TRADE CLOSED: Gross PnL: ₹{}, Taxes: ₹{}, Net PnL: ₹{}, New Balance: ₹{}",
            charges.grossPnL(), charges.totalCharges(), charges.netPnL(), virtualCash.get());

        if (broadcastServiceProvider != null) {
            var broadcaster = broadcastServiceProvider.getIfAvailable();
            if (broadcaster != null) {
                try {
                    broadcaster.broadcastOverview();
                } catch (Throwable t) {
                    log.warn("Failed to broadcast overview update after paper trade close: {}", t.getMessage());
                }
            }
        }
    }

    @Override
    public Optional<Position> getOpenPosition() {
        return Optional.ofNullable(openPosition.get());
    }

    @Override
    public double getAvailableCash() {
        Position current = openPosition.get();
        double lockedMargin = (current != null) ? calculateOrderMargin(current.symbol(), current.quantity(), current.entryPrice()) : 0.0;
        return Math.max(0.0, virtualCash.get() - lockedMargin);
    }

    @Override
    public double getTotalCapital() {
        return Math.round(virtualCash.get() * 100.0) / 100.0;
    }

    @Override
    public double getBaseCapital() {
        return baseCapital;
    }

    @Override
    public double getCumulativeNetPnL() {
        return Math.round((virtualCash.get() - baseCapital) * 100.0) / 100.0;
    }

    @Override
    public double calculateOrderMargin(String symbol, int quantity, double price) {
        // Models 20% margin (5x intraday leverage) in paper trading
        return (quantity * price) * 0.20;
    }

    @Override
    public ExecutionMode getExecutionMode() {
        return ExecutionMode.PAPER;
    }

    @Override
    public java.util.Map<String, Object> getStockQuote(String rawSymbol) {
        if (zerodhaLiveGatewayProvider != null) {
            var liveGateway = zerodhaLiveGatewayProvider.getIfAvailable();
            if (liveGateway != null) {
                java.util.Map<String, Object> liveQuote = liveGateway.getStockQuote(rawSymbol);
                if (liveQuote != null && liveQuote.get("lastPrice") instanceof Number) {
                    return liveQuote;
                }
            }
        }
        String cleanSymbol = rawSymbol.trim().toUpperCase().replace("NSE:", "");
        java.util.Map<String, Object> result = new java.util.LinkedHashMap<>();
        result.put("symbol", cleanSymbol);
        result.put("instrument", "NSE:" + cleanSymbol);
        result.put("authenticated", false);
        result.put("error", "ZERODHA_SESSION_REQUIRED");
        result.put("message", "Zerodha Kite session is not active. Please authenticate your Zerodha account to stream live quotes from NSE.");
        return result;
    }

    @Override
    public TradeOrder getOrderStatus(String orderId) {
        return orders.get(orderId);
    }

    @Override
    public boolean hasActiveOrders() {
        return orders.values().stream()
            .anyMatch(o -> "SUBMITTED".equalsIgnoreCase(o.status()) 
                        || "PENDING".equalsIgnoreCase(o.status()) 
                        || "TRIGGER PENDING".equalsIgnoreCase(o.status()));
    }
}
