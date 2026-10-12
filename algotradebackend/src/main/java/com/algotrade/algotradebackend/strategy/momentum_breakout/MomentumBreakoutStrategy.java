package com.algotrade.algotradebackend.strategy.momentum_breakout;

import com.algotrade.algotradebackend.broker.BrokerGateway;
import com.algotrade.algotradebackend.broker.BrokerGatewayManager;
import com.algotrade.algotradebackend.core.model.Position;
import com.algotrade.algotradebackend.core.model.ScoredCandidate;
import com.algotrade.algotradebackend.core.model.TradeOrder;
import com.algotrade.algotradebackend.risk.PositionSizingEngine;
import com.algotrade.algotradebackend.strategy.TradingStrategy;
import com.algotrade.algotradebackend.core.service.MarketCalendarService;
import com.algotrade.algotradebackend.core.service.EngineActivityFeedService;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Service;

import java.time.LocalDate;
import java.time.LocalTime;
import java.time.ZoneId;
import java.util.*;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CopyOnWriteArrayList;

/**
 * Feature Implementation: Momentum Breakout Strategy.
 * 
 * <p>Lifecycle:</p>
 * <ol>
 *   <li><b>09:00:00 AM:</b> Automatic daily session reset of candidate metrics, baselines, and state machine.</li>
 *   <li><b>09:15–09:30 AM:</b> Ingests ticks and accumulates metrics across liquid watchlist.</li>
 *   <li><b>09:30:00 AM:</b> Scores candidates via 100-pt rubric. Enforces hard eligibility gates (PDH breakout + VWAP).</li>
 *   <li><b>09:30:05 AM:</b> Sizes position using 3 constraints (min Q_risk, Q_margin, Q_exposure). Places entry limit.</li>
 *   <li><b>Entry Fill:</b> Once broker confirms fill, arms protective stop-loss for exact confirmed quantity.</li>
 *   <li><b>09:30–11:00 AM:</b> Phase 1 "Let it breathe" (retains initial structural stop at 15-min low).</li>
 *   <li><b>11:00–15:05 PM:</b> Phase 2 Trailing buffer (ratchets stop at 2% below persistent High-Water Mark).</li>
 *   <li><b>15:05:00 PM:</b> Phase 3 Mandatory square-off. Cancels pending entry/SL orders and exits position ahead of Zerodha CAS 15:12 cutoff.</li>
 * </ol>
 */
@Service
public class MomentumBreakoutStrategy implements TradingStrategy {

    private static final Logger log = LoggerFactory.getLogger(MomentumBreakoutStrategy.class);

    private final MomentumScoringEngine scoringEngine;
    private final PositionSizingEngine positionSizingEngine;
    private final BrokerGatewayManager brokerGatewayManager;
    private final MarketCalendarService marketCalendarService;
    private final EngineActivityFeedService activityFeedService;

    private final ConcurrentHashMap<String, CandidateMetrics> metricsMap = new ConcurrentHashMap<>();
    private final CopyOnWriteArrayList<ScoredCandidate> latestTopCandidates = new CopyOnWriteArrayList<>();

    // Active trade tracking & lifecycle state machine
    private volatile TradeLifecycleState lifecycleState = TradeLifecycleState.FLAT;
    private volatile String activeSymbol = null;
    private volatile String activeEntryOrderId = null;
    private volatile String activeSlOrderId = null;
    private volatile String activeTradeId = null;
    private volatile double strategyHighWaterMark = 0.0;
    private volatile double plannedStopPrice = 0.0;
    private volatile int plannedQuantity = 0;
    private volatile LocalDate lastSessionDate = null;

    public MomentumBreakoutStrategy(
        MomentumScoringEngine scoringEngine,
        PositionSizingEngine positionSizingEngine,
        BrokerGatewayManager brokerGatewayManager,
        MarketCalendarService marketCalendarService,
        EngineActivityFeedService activityFeedService
    ) {
        this.scoringEngine = scoringEngine;
        this.positionSizingEngine = positionSizingEngine;
        this.brokerGatewayManager = brokerGatewayManager;
        this.marketCalendarService = marketCalendarService;
        this.activityFeedService = activityFeedService;
        initializeDefaultWatchlist();
    }

    private void initializeDefaultWatchlist() {
        // High-liquidity Nifty 100 constituents. Reference data (prevClose, PDH, volume) populated dynamically.
        List<String> defaultSymbols = List.of(
            "NTPC", "RELIANCE", "INFY", "TCS", "HDFCBANK",
            "ICICIBANK", "SBIN", "BHARTIARTL", "LT", "AXISBANK"
        );
        for (String sym : defaultSymbols) {
            metricsMap.put(sym, new CandidateMetrics(sym));
        }
    }

    // Directional tick-size rounding helpers (5 paise / 0.05 increments)
    private static double ceilToTick(double price) {
        return Math.ceil(price * 20.0) / 20.0;
    }

    private static double floorToTick(double price) {
        return Math.floor(price * 20.0) / 20.0;
    }

    /**
     * Resets observations and trade lifecycle for a fresh trading day session.
     */
    @Scheduled(cron = "0 0 9 * * MON-FRI", zone = "Asia/Kolkata")
    public synchronized void resetDailySession() {
        log.info("Resetting daily session for MomentumBreakoutStrategy...");
        for (CandidateMetrics m : metricsMap.values()) {
            m.resetSession();
        }
        latestTopCandidates.clear();
        lifecycleState = TradeLifecycleState.FLAT;
        activeSymbol = null;
        activeEntryOrderId = null;
        activeSlOrderId = null;
        activeTradeId = null;
        strategyHighWaterMark = 0.0;
        plannedStopPrice = 0.0;
        plannedQuantity = 0;
        lastSessionDate = LocalDate.now(ZoneId.of("Asia/Kolkata"));
        activityFeedService.record(
            "SYSTEM",
            "INFO",
            "Daily Session Reset",
            "Reset morning candidate metrics, VWAP baselines, and trade lifecycle state for new session."
        );
    }

    private void ensureSessionCurrent() {
        LocalDate today = LocalDate.now(ZoneId.of("Asia/Kolkata"));
        if (lastSessionDate == null || !lastSessionDate.equals(today)) {
            resetDailySession();
        }
    }

    /**
     * Dynamically updates daily reference figures fetched from the broker market data API.
     */
    public void updateReferenceData(String symbol, double prevClose, double pdh, double avgVol, double atrPct) {
        CandidateMetrics m = metricsMap.computeIfAbsent(symbol, CandidateMetrics::new);
        m.setReferenceData(prevClose, pdh, avgVol, atrPct);
    }

    @Override
    public String getStrategyId() {
        return "MOMENTUM_BREAKOUT";
    }

    @Override
    public String getStrategyName() {
        return "15-Min Opening Momentum Breakout";
    }

    @Override
    public void onTick(String symbol, double ltp, long volume, long totalBuyQty, long totalSellQty) {
        LocalTime now = LocalTime.now(ZoneId.of("Asia/Kolkata"));
        boolean inObservationWindow = marketCalendarService.isObservationWindow();
        onTick(symbol, ltp, volume, totalBuyQty, totalSellQty, inObservationWindow, now);
    }

    /**
     * Overload supporting deterministic time injection for testing and market simulations.
     */
    public void onTick(String symbol, double ltp, long volume, long totalBuyQty, long totalSellQty, boolean inObservationWindow, LocalTime currentTime) {
        ensureSessionCurrent();

        // (i) Stop stock observation and tick processing during off-market hours
        if (!marketCalendarService.isMarketOpen()) {
            return;
        }

        CandidateMetrics metrics = metricsMap.get(symbol);
        if (metrics != null) {
            metrics.updateTick(ltp, volume, totalBuyQty, totalSellQty, inObservationWindow);
        }

        // Active Trade Trailing & Lifecycle Management
        if (symbol.equals(activeSymbol)) {
            manageActiveTradeLifecycle(symbol, ltp, currentTime);
        }
    }

    private synchronized void manageActiveTradeLifecycle(String symbol, double ltp, LocalTime now) {
        BrokerGateway gateway = brokerGatewayManager.getActiveGateway();
        Optional<Position> optPos = gateway.getOpenPosition();

        if (lifecycleState == TradeLifecycleState.PENDING_ENTRY) {
            // Waiting for broker to confirm fill of buy order
            if (optPos.isPresent()) {
                Position pos = optPos.get();
                if (pos.symbol().equals(symbol) && pos.quantity() > 0) {
                    lifecycleState = TradeLifecycleState.ACTIVE_POSITION;
                    strategyHighWaterMark = Math.max(pos.entryPrice(), ltp);

                    // Place protective stop-loss for the confirmed filled quantity
                    double slLimitPrice = floorToTick(plannedStopPrice * 0.998);
                    double triggerPrice = floorToTick(plannedStopPrice);
                    try {
                        TradeOrder slOrder = gateway.placeOrder(symbol, "SELL", pos.quantity(), slLimitPrice, triggerPrice, "SL");
                        activeSlOrderId = slOrder.orderId();
                        log.info("PROTECTIVE SL CONFIRMED & ARMED: OrderId={}, Qty={}, Trigger=₹{}, Limit=₹{}",
                            activeSlOrderId, pos.quantity(), triggerPrice, slLimitPrice);
                        activityFeedService.record(
                            "ORDERS",
                            "SUCCESS",
                            gateway.getExecutionMode().name(),
                            activeTradeId,
                            "Protective Stop-Loss Armed",
                            "Position confirmed (" + pos.quantity() + " shares). SL armed: Trigger=₹" + triggerPrice + ", Limit=₹" + slLimitPrice,
                            symbol,
                            Map.of("slOrderId", activeSlOrderId, "triggerPrice", triggerPrice, "limitPrice", slLimitPrice, "quantity", pos.quantity())
                        );
                    } catch (Throwable t) {
                        log.error("CRITICAL: Failed to arm protective stop for filled position {}: {}", symbol, t.getMessage(), t);
                        activityFeedService.record(
                            "ALERT",
                            "ERROR",
                            gateway.getExecutionMode().name(),
                            activeTradeId,
                            "Emergency: Stop-Loss Failed",
                            "Failed to place protective SL for " + symbol + ": " + t.getMessage(),
                            symbol
                        );
                    }
                }
            }
            // Do NOT clear activeSymbol while pending fill
            return;
        }

        if (lifecycleState == TradeLifecycleState.ACTIVE_POSITION) {
            if (optPos.isPresent()) {
                Position pos = optPos.get();
                // Update persistent high-water mark monotonically (never drops on pullbacks)
                strategyHighWaterMark = Math.max(strategyHighWaterMark, ltp);

                // Phase 2: Trailing Buffer active only after 11:00 AM IST
                if (now.isAfter(LocalTime.of(11, 0)) && activeSlOrderId != null) {
                    // Trail 2.0% below the highest price achieved since entry
                    double targetTrailStop = floorToTick(strategyHighWaterMark * 0.980);

                    // Ratchet stop strictly upward if higher than current stop and above initial stop
                    if (targetTrailStop > pos.activeTrailingStop() && targetTrailStop > plannedStopPrice) {
                        double limitPrice = floorToTick(targetTrailStop * 0.998);
                        log.info("RATCHETING TRAIL STOP for {}: Persistent HWM=₹{}, New Trigger=₹{}, Limit=₹{}",
                            symbol, strategyHighWaterMark, targetTrailStop, limitPrice);
                        gateway.modifyOrder(activeSlOrderId, limitPrice, targetTrailStop);
                        activityFeedService.record(
                            "ORDERS",
                            "SUCCESS",
                            gateway.getExecutionMode().name(),
                            activeTradeId,
                            "Trailing Stop Ratcheted",
                            "Persistent HWM updated to ₹" + strategyHighWaterMark + ". Trailing stop raised to ₹" + targetTrailStop + " (Limit: ₹" + limitPrice + ")",
                            symbol,
                            Map.of("highWaterMark", strategyHighWaterMark, "triggerPrice", targetTrailStop, "limitPrice", limitPrice)
                        );
                    }
                }
            } else {
                // Position closed (stop-loss was hit or manually closed)
                log.info("Position for {} is no longer open at broker. Trade completed.", symbol);
                activityFeedService.record(
                    "LIFECYCLE",
                    "INFO",
                    gateway.getExecutionMode().name(),
                    activeTradeId,
                    "Trade Closed",
                    "Position for " + symbol + " closed at broker (Stop-Loss filled or squared off).",
                    symbol
                );
                lifecycleState = TradeLifecycleState.FLAT;
                activeSymbol = null;
                activeSlOrderId = null;
                activeTradeId = null;
                strategyHighWaterMark = 0.0;
            }
            return;
        }

        if (lifecycleState == TradeLifecycleState.PENDING_EXIT) {
            // Waiting for square-off exit to confirm flat
            if (optPos.isEmpty() || (optPos.get().quantity() == 0)) {
                log.info("Square-off exit confirmed flat for {}.", symbol);
                lifecycleState = TradeLifecycleState.FLAT;
                activeSymbol = null;
                activeSlOrderId = null;
                activeTradeId = null;
                strategyHighWaterMark = 0.0;
            }
        }
    }

    @Override
    public synchronized void evaluate0930() {
        ensureSessionCurrent();
        log.info("=== RUNNING 09:30:00 SCORING EVALUATION ===");
        activityFeedService.record(
            "STRATEGY",
            "INFO",
            "09:30 Evaluation Started",
            "Evaluating 15-min momentum breakout metrics across liquid Nifty watchlist."
        );
        BrokerGateway gateway = brokerGatewayManager.getActiveGateway();

        // Safety Gate: Ensure no position is already open and daily limit is not reached
        if (gateway.getOpenPosition().isPresent() || lifecycleState != TradeLifecycleState.FLAT) {
            log.warn("Evaluation skipped: A position or order is already actively held.");
            activityFeedService.record(
                "STRATEGY",
                "WARN",
                "Evaluation Skipped",
                "A trade position or order is already active. Engine strictly enforces 1 active position."
            );
            return;
        }

        if (!positionSizingEngine.canTradeToday()) {
            log.warn("Evaluation skipped: 1-trade-per-day limit already reached.");
            activityFeedService.record(
                "RISK",
                "WARN",
                "Daily Trade Limit Enforced",
                "Strict 1-trade-per-day limit reached for today. Standing down until tomorrow's session."
            );
            return;
        }

        // Step 1: Score all candidates (atomically captures snapshot per candidate)
        List<ScoredCandidate> scoredList = metricsMap.values().stream()
            .map(scoringEngine::evaluate)
            .toList();

        // Step 2: Rank candidates with tie-breaker
        List<ScoredCandidate> ranked = scoringEngine.rankCandidates(scoredList);
        latestTopCandidates.clear();
        latestTopCandidates.addAll(ranked);

        log.info("Scoring complete. Top candidate: {} with Score {}/100",
            ranked.isEmpty() ? "NONE" : ranked.get(0).symbol(),
            ranked.isEmpty() ? 0 : ranked.get(0).totalScore());

        // Step 3: Check qualification threshold (Score >= 75 and passes hard gates)
        if (ranked.isEmpty() || !ranked.get(0).isQualified()) {
            String bestSym = ranked.isEmpty() ? "NONE" : ranked.get(0).symbol();
            int bestScore = ranked.isEmpty() ? 0 : ranked.get(0).totalScore();
            log.warn("NO TRADE TODAY: Top candidate did not meet the 75-point qualification threshold.");
            activityFeedService.record(
                "STRATEGY",
                "WARN",
                "No Qualified Candidates",
                "Top candidate was " + bestSym + " with score " + bestScore + "/100. Minimum threshold is 75 (with mandatory PDH breakout and above VWAP). No trade executed.",
                bestSym,
                Map.of("topScore", bestScore, "threshold", 75, "totalEvaluated", ranked.size())
            );
            return;
        }

        ScoredCandidate winner = ranked.get(0);
        CandidateMetrics winnerMetrics = metricsMap.get(winner.symbol());

        activityFeedService.record(
            "STRATEGY",
            "SUCCESS",
            "Breakout Candidate Qualified",
            winner.symbol() + " passed qualification with score " + winner.totalScore() + "/100 >= 75.",
            winner.symbol(),
            Map.of(
                "totalScore", winner.totalScore(),
                "rvolScore", winner.rvolScore(),
                "vwapScore", winner.vwapScore(),
                "depthScore", winner.depthImbalanceScore(),
                "pdhScore", winner.pdhScore(),
                "ltp", winner.ltp()
            )
        );

        // Step 4: Calculate Structural Stop and Position Size
        double entryPrice = winner.ltp();
        double effLow15 = winnerMetrics.getLow15();
        // Initial stop strictly anchored to 15-min low minus 5 paise tick buffer, directionally floored
        double initialStop = floorToTick(effLow15 - 0.05);

        if (initialStop <= 0 || initialStop >= entryPrice) {
            log.warn("Trade aborted for {}: Invalid stop price ₹{} (Entry ~₹{})", winner.symbol(), initialStop, entryPrice);
            activityFeedService.record(
                "RISK",
                "WARN",
                "Trade Aborted: Invalid Stop",
                "Stop loss ₹" + initialStop + " is invalid relative to entry ₹" + entryPrice,
                winner.symbol()
            );
            return;
        }

        int quantity = positionSizingEngine.calculateQuantity(winner.symbol(), entryPrice, initialStop, gateway);
        if (quantity < 1) {
            log.warn("Trade aborted for {}: Calculated quantity is 0 shares.", winner.symbol());
            activityFeedService.record(
                "RISK",
                "WARN",
                "Trade Aborted: 0 Shares",
                "Risk constraints or stop distance resulted in 0 shares calculated for " + winner.symbol(),
                winner.symbol()
            );
            return;
        }

        activityFeedService.record(
            "RISK",
            "INFO",
            "Position Sizing Computed",
            "Calculated size: " + quantity + " shares. Entry ~₹" + entryPrice + ", Initial Stop: ₹" + initialStop,
            winner.symbol(),
            Map.of("quantity", quantity, "entryPrice", entryPrice, "initialStop", initialStop)
        );

        // Step 5: Place Marketable Limit Buy Order
        String tradeId = "TRD-" + LocalDate.now(ZoneId.of("Asia/Kolkata")) + "-" + winner.symbol();
        activeTradeId = tradeId;
        activeSymbol = winner.symbol();
        plannedStopPrice = initialStop;
        plannedQuantity = quantity;
        lifecycleState = TradeLifecycleState.PENDING_ENTRY;

        double marketableBuyLimit = ceilToTick(entryPrice * 1.0005); // LTP + 0.05% ceil to valid tick
        log.info("DISPATCHING ENTRY ORDER: BUY {} x {} @ Marketable Limit ₹{}", quantity, winner.symbol(), marketableBuyLimit);

        TradeOrder buyOrder = gateway.placeOrder(winner.symbol(), "BUY", quantity, marketableBuyLimit, 0.0, "LIMIT");
        activeEntryOrderId = buyOrder.orderId();
        activityFeedService.record(
            "ORDERS",
            "SUCCESS",
            gateway.getExecutionMode().name(),
            activeTradeId,
            "Entry Order Dispatched",
            "BUY " + quantity + " x " + winner.symbol() + " @ Marketable Limit ₹" + marketableBuyLimit,
            winner.symbol(),
            Map.of("orderId", buyOrder.orderId(), "quantity", quantity, "limitPrice", marketableBuyLimit)
        );

        // Step 6: If gateway immediately confirmed filled position (e.g. Paper Trading), arm protective SL immediately
        Optional<Position> immediatePos = gateway.getOpenPosition();
        if (immediatePos.isPresent() && immediatePos.get().symbol().equals(winner.symbol())) {
            lifecycleState = TradeLifecycleState.ACTIVE_POSITION;
            strategyHighWaterMark = Math.max(entryPrice, immediatePos.get().currentPrice());
            double slLimitPrice = floorToTick(initialStop * 0.998);
            TradeOrder slOrder = gateway.placeOrder(winner.symbol(), "SELL", quantity, slLimitPrice, initialStop, "SL");
            activeSlOrderId = slOrder.orderId();

            log.info("PROTECTIVE SL ENGAGED: OrderId={}, Trigger=₹{}, Limit=₹{}", activeSlOrderId, initialStop, slLimitPrice);
            activityFeedService.record(
                "ORDERS",
                "SUCCESS",
                gateway.getExecutionMode().name(),
                activeTradeId,
                "Protective Stop-Loss Armed",
                "Stop-Loss order engaged: Trigger=₹" + initialStop + ", Limit=₹" + slLimitPrice,
                winner.symbol(),
                Map.of("slOrderId", activeSlOrderId, "triggerPrice", initialStop, "limitPrice", slLimitPrice)
            );
        } else {
            log.info("Entry order dispatched. Waiting for fill confirmation before placing protective stop...");
        }
    }

    @Override
    @Scheduled(cron = "0 5 15 * * MON-FRI", zone = "Asia/Kolkata")
    public synchronized void executeSquareOff() {
        log.info("=== 15:05:00 MANDATORY INTRADAY SQUARE-OFF ENGAGED ===");
        BrokerGateway gateway = brokerGatewayManager.getActiveGateway();

        // 1. Reconcile in-flight Entry Order: Cancel pending buy order to prevent late execution
        if (activeEntryOrderId != null && lifecycleState == TradeLifecycleState.PENDING_ENTRY) {
            log.warn("CANCELLING RESTING ENTRY ORDER AT 15:05 CUTOFF: OrderId={}", activeEntryOrderId);
            try {
                gateway.cancelOrder(activeEntryOrderId);
                activityFeedService.record(
                    "ORDERS",
                    "WARN",
                    gateway.getExecutionMode().name(),
                    activeTradeId,
                    "Pending Entry Order Cancelled",
                    "Cancelled unfilled entry order " + activeEntryOrderId + " at 15:05 square-off cutoff.",
                    activeSymbol,
                    Map.of("entryOrderId", activeEntryOrderId, "reason", "15:05_EOD_CUTOFF")
                );
            } catch (Throwable t) {
                log.error("Failed to cancel pending entry order {}: {}", activeEntryOrderId, t.getMessage());
            }
            activeEntryOrderId = null;
            lifecycleState = TradeLifecycleState.FLAT;
            activeSymbol = null;
            activeTradeId = null;
            strategyHighWaterMark = 0.0;
            return;
        }

        // 2. Cancel resting stop-loss order first
        if (activeSlOrderId != null) {
            try {
                gateway.cancelOrder(activeSlOrderId);
            } catch (Throwable t) {
                log.warn("Failed to cancel resting SL order {}: {}", activeSlOrderId, t.getMessage());
            }
            activeSlOrderId = null;
        }

        Optional<Position> optPos = gateway.getOpenPosition();
        if (optPos.isPresent()) {
            Position pos = optPos.get();
            log.warn("SQUARING OFF ACTIVE POSITION: {} x {}", pos.quantity(), pos.symbol());
            activityFeedService.record(
                "LIFECYCLE",
                "WARN",
                gateway.getExecutionMode().name(),
                activeTradeId,
                "15:05 Mandatory Square-Off",
                "Closing active position " + pos.symbol() + " (" + pos.quantity() + " shares) ahead of broker CAS cutoffs.",
                pos.symbol(),
                Map.of("quantity", pos.quantity(), "entryPrice", pos.entryPrice(), "currentPrice", pos.currentPrice())
            );

            lifecycleState = TradeLifecycleState.PENDING_EXIT;

            // Aggressive limit sell: LTP - 0.10% (directionally floored)
            double aggressiveSellPrice = floorToTick(pos.currentPrice() * 0.9990);
            gateway.placeOrder(pos.symbol(), "SELL", pos.quantity(), aggressiveSellPrice, 0.0, "LIMIT");

            // Check if immediately flat (e.g. Paper Trading)
            Optional<Position> postExit = gateway.getOpenPosition();
            if (postExit.isEmpty()) {
                lifecycleState = TradeLifecycleState.FLAT;
                activeSymbol = null;
                activeTradeId = null;
                strategyHighWaterMark = 0.0;
            }
        } else {
            log.info("Square-off complete: No open position found.");
            lifecycleState = TradeLifecycleState.FLAT;
            activeSymbol = null;
            activeTradeId = null;
            strategyHighWaterMark = 0.0;
            activityFeedService.record(
                "LIFECYCLE",
                "INFO",
                "15:05 Square-Off Check",
                "Square-off triggered: No open positions to exit."
            );
        }
    }

    @Override
    public synchronized void executeSquareOff1515() {
        executeSquareOff();
    }

    @Override
    public List<ScoredCandidate> getLatestTopCandidates() {
        return Collections.unmodifiableList(latestTopCandidates);
    }

    public Map<String, CandidateMetrics> getMetricsMap() {
        return Collections.unmodifiableMap(metricsMap);
    }

    public TradeLifecycleState getLifecycleState() {
        return lifecycleState;
    }

    public double getStrategyHighWaterMark() {
        return strategyHighWaterMark;
    }

    public String getActiveSymbol() {
        return activeSymbol;
    }

    public String getActiveTradeId() {
        return activeTradeId;
    }
}
