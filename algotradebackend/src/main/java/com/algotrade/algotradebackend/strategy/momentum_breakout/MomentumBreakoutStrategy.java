package com.algotrade.algotradebackend.strategy.momentum_breakout;

import com.algotrade.algotradebackend.broker.BrokerGateway;
import com.algotrade.algotradebackend.broker.BrokerGatewayManager;
import com.algotrade.algotradebackend.core.model.ChargesBreakdown;
import com.algotrade.algotradebackend.core.model.Position;
import com.algotrade.algotradebackend.core.model.ScoredCandidate;
import com.algotrade.algotradebackend.core.model.TradeOrder;
import com.algotrade.algotradebackend.core.util.TickMath;
import com.algotrade.algotradebackend.risk.PositionSizingEngine;
import com.algotrade.algotradebackend.strategy.TradingStrategy;
import com.algotrade.algotradebackend.core.service.MarketCalendarService;
import com.algotrade.algotradebackend.core.service.EngineActivityFeedService;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
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
 *   <li><b>Phase 1: Initial & Breakeven:</b> Retains initial structural stop at 15-min low, until +1.0% gain milestone triggers exact statutory breakeven lock.</li>
 *   <li><b>Phase 2: Progressive Trailing:</b> Once persistent High-Water Mark reaches +1.5% gain, ratchets stop 0.8% below HWM.</li>
 *   <li><b>Phase 3: 15:05:00 PM Square-Off:</b> Cancels pending entry/SL orders and exits position ahead of Zerodha CAS 15:12 cutoff.</li>
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

    @Value("${algotrade.strategy.breakeven-trigger-pct:1.0}")
    private double breakevenTriggerPct = 1.0;

    @Value("${algotrade.strategy.breakeven-cost-buffer-pct:0.15}")
    private double breakevenCostBufferPct = 0.15;

    @Value("${algotrade.strategy.trailing-buffer-pct:0.8}")
    private double trailingBufferPct = 0.8;

    @Value("${algotrade.strategy.max-support-distance-pct:1.5}")
    private double maxSupportDistancePct = 1.5;

    private final ConcurrentHashMap<String, CandidateMetrics> metricsMap = new ConcurrentHashMap<>();
    private final CopyOnWriteArrayList<ScoredCandidate> latestTopCandidates = new CopyOnWriteArrayList<>();
    private final java.util.Set<String> tradedSymbolsToday = java.util.concurrent.ConcurrentHashMap.newKeySet();

    // Active trade tracking & lifecycle state machine
    private volatile TradeLifecycleState lifecycleState = TradeLifecycleState.FLAT;
    private volatile String activeSymbol = null;
    private volatile String activeEntryOrderId = null;
    private volatile String activeSlOrderId = null;
    private volatile String activeTradeId = null;
    private volatile double strategyHighWaterMark = 0.0;
    private volatile double plannedStopPrice = 0.0;
    private volatile double currentArmedStopPrice = 0.0;
    private volatile int plannedQuantity = 0;
    private volatile LocalDate lastSessionDate = null;
    private volatile LocalTime lastTickTime = null;

    public void setTrailingParameters(double breakevenTriggerPct, double breakevenCostBufferPct, double trailingBufferPct) {
        this.breakevenTriggerPct = breakevenTriggerPct;
        this.breakevenCostBufferPct = breakevenCostBufferPct;
        this.trailingBufferPct = trailingBufferPct;
    }

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

    @org.springframework.beans.factory.annotation.Autowired(required = false)
    private com.algotrade.algotradebackend.persistence.repository.MarketUniverseRepository universeRepository;

    @org.springframework.beans.factory.annotation.Autowired(required = false)
    private org.springframework.beans.factory.ObjectProvider<com.algotrade.algotradebackend.websocket.LiveWebSocketBroadcastService> broadcastServiceProvider;

    private final java.util.concurrent.atomic.AtomicBoolean evaluatingTrigger = new java.util.concurrent.atomic.AtomicBoolean(false);
    private final ConcurrentHashMap<String, Long> lastRadarEvaluationTimeMs = new ConcurrentHashMap<>();
    private final java.util.concurrent.atomic.AtomicLong lastGlobalRadarCycleMs = new java.util.concurrent.atomic.AtomicLong(0);

    @jakarta.annotation.PostConstruct
    public void populateFullUniverseWatchlist() {
        if (universeRepository != null) {
            try {
                var list = universeRepository.findByIsFnoTrueOrderBySymbolAsc();
                if (list != null && !list.isEmpty()) {
                    for (var e : list) {
                        CandidateMetrics m = metricsMap.computeIfAbsent(e.getSymbol(), CandidateMetrics::new);
                        if (e.getTickSize() != null && e.getTickSize() > 0.0) {
                            m.setTickSize(e.getTickSize());
                        }
                    }
                    log.info("Initialized MomentumBreakoutStrategy metricsMap with {} F&O constituents from database.", list.size());
                }
            } catch (Throwable t) {
                log.warn("Could not pre-populate full universe into metricsMap: {}", t.getMessage());
            }
        }
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

    public double getSymbolTickSize(String symbol) {
        if (symbol != null) {
            CandidateMetrics m = metricsMap.get(symbol);
            if (m != null) {
                return m.getTickSize();
            }
        }
        return 0.05;
    }

    private double ceilToTick(double price) {
        return TickMath.ceilToTick(price, activeSymbol != null ? getSymbolTickSize(activeSymbol) : 0.05);
    }

    private double floorToTick(double price) {
        return TickMath.floorToTick(price, activeSymbol != null ? getSymbolTickSize(activeSymbol) : 0.05);
    }

    /**
     * Resets observations and trade lifecycle for a fresh trading day session.
     */
    @Scheduled(cron = "0 0 9 * * MON-FRI", zone = "Asia/Kolkata")
    public synchronized void resetDailySession() {
        log.info("Resetting daily session for MomentumBreakoutStrategy...");
        lastSessionDate = LocalDate.now(ZoneId.of("Asia/Kolkata"));
        for (CandidateMetrics m : metricsMap.values()) {
            m.resetSession();
        }
        latestTopCandidates.clear();
        tradedSymbolsToday.clear();
        lastRadarEvaluationTimeMs.clear();
        lastTickTime = null;

        // Safety check: Do not blindly wipe active state if an unsettled position is still open at the broker
        BrokerGateway gateway = brokerGatewayManager.getActiveGateway();
        try {
            Optional<Position> openPos = gateway.getOpenPosition();
            if (openPos.isPresent() && openPos.get().quantity() > 0) {
                Position p = openPos.get();
                log.warn("SESSION RESET FOUND UNCLOSED POSITION: {} x {}. Retaining trade state.", p.quantity(), p.symbol());
                activeSymbol = p.symbol();
                lifecycleState = TradeLifecycleState.ACTIVE_POSITION;
                strategyHighWaterMark = Math.max(strategyHighWaterMark, p.currentPrice());
                return;
            }
        } catch (Throwable t) {
            log.warn("Could not query broker positions during session reset: {}", t.getMessage());
        }

        lifecycleState = TradeLifecycleState.FLAT;
        activeSymbol = null;
        activeEntryOrderId = null;
        activeSlOrderId = null;
        activeTradeId = null;
        strategyHighWaterMark = 0.0;
        plannedStopPrice = 0.0;
        currentArmedStopPrice = 0.0;
        plannedQuantity = 0;
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
     * Replaces the daily observation candidate watchlist with dynamically shortlisted pre-market candidates.
     * Preserves active trade symbol if a trade is already open or pending.
     */
    public synchronized void setPreMarketWatchlist(List<String> symbols) {
        if (symbols == null || symbols.isEmpty()) {
            return;
        }

        CandidateMetrics activeMetrics = (activeSymbol != null) ? metricsMap.get(activeSymbol) : null;
        metricsMap.clear();

        if (activeMetrics != null && activeSymbol != null) {
            metricsMap.put(activeSymbol, activeMetrics);
        }

        for (String sym : symbols) {
            metricsMap.computeIfAbsent(sym, CandidateMetrics::new);
        }

        log.info("MomentumBreakoutStrategy watchlist focused to {} pre-market candidates: {}",
            metricsMap.size(), metricsMap.keySet());
    }

    /**
     * Dynamically updates daily reference figures fetched from the broker market data API.
     */
    public void updateReferenceData(String symbol, double prevClose, double pdh, double avgVol, double atrPct) {
        CandidateMetrics m = metricsMap.computeIfAbsent(symbol, CandidateMetrics::new);
        m.setReferenceData(prevClose, pdh, avgVol, atrPct);
    }

    /**
     * Sets the official 09:15:00 window-boundary volume baseline to prevent excluding opening volume.
     */
    public void setOpeningVolumeBaseline(String symbol, long baselineVolume) {
        CandidateMetrics m = metricsMap.computeIfAbsent(symbol, CandidateMetrics::new);
        m.setOpeningVolumeBaseline(baselineVolume);
    }

    /**
     * Ingests 1-minute aggregated candle data with observation window boundary enforcement.
     */
    public void aggregateCandle(String symbol, LocalTime candleTime, double open, double high, double low, double close, long candleVolume) {
        ensureSessionCurrent();
        if (candleTime.isBefore(MarketCalendarService.MARKET_OPEN_TIME) || !candleTime.isBefore(MarketCalendarService.OBSERVATION_END_TIME)) {
            return;
        }
        CandidateMetrics m = metricsMap.computeIfAbsent(symbol, CandidateMetrics::new);
        m.aggregateCandle(open, high, low, close, candleVolume);
    }

    public void aggregateCandle(String symbol, double open, double high, double low, double close, long candleVolume) {
        if (!marketCalendarService.isObservationWindow()) {
            return;
        }
        ensureSessionCurrent();
        CandidateMetrics m = metricsMap.computeIfAbsent(symbol, CandidateMetrics::new);
        m.aggregateCandle(open, high, low, close, candleVolume);
    }

    @Override
    public String getStrategyId() {
        return "MOMENTUM_BREAKOUT";
    }

    @Override
    public String getStrategyName() {
        return "All-Day Continuous Breakout Engine";
    }

    @Override
    public void onTick(String symbol, double ltp, long volume, long totalBuyQty, long totalSellQty) {
        LocalTime now = LocalTime.now(ZoneId.of("Asia/Kolkata"));
        boolean inObservationWindow = marketCalendarService.isObservationWindow();
        onTick(symbol, ltp, volume, totalBuyQty, totalSellQty, 0.0, inObservationWindow, now);
    }

    /**
     * Overload supporting deterministic time injection for testing and market simulations.
     */
    public void onTick(String symbol, double ltp, long volume, long totalBuyQty, long totalSellQty, boolean inObservationWindow, LocalTime currentTime) {
        onTick(symbol, ltp, volume, totalBuyQty, totalSellQty, 0.0, inObservationWindow, currentTime);
    }

    /**
     * Overload supporting official exchange-calculated VWAP (ATP) and deterministic time injection.
     */
    public void onTick(String symbol, double ltp, long volume, long totalBuyQty, long totalSellQty, double exchangeVwap, boolean inObservationWindow, LocalTime currentTime) {
        ensureSessionCurrent();

        // (i) Stop stock observation and tick processing during off-market hours
        if (!marketCalendarService.isMarketOpen()) {
            return;
        }

        this.lastTickTime = currentTime;
        CandidateMetrics metrics = metricsMap.computeIfAbsent(symbol, CandidateMetrics::new);
        metrics.updateTick(ltp, volume, totalBuyQty, totalSellQty, exchangeVwap, inObservationWindow);

        // Real-Time Breakout Radar:
        // Evaluates breakout signal immediately when price breaks above PDH
        checkRealtimeBreakoutTrigger(symbol, metrics, ltp, currentTime);

        // Active Trade Trailing & Lifecycle Management
        if (symbol.equals(activeSymbol)) {
            manageActiveTradeLifecycle(symbol, ltp, currentTime);
        }
    }

    private void checkRealtimeBreakoutTrigger(String symbol, CandidateMetrics metrics, double ltp, LocalTime currentTime) {
        // Session Window Gate: 09:20 AM to 02:45 PM IST on active market trading days
        if (currentTime.isBefore(LocalTime.of(9, 20)) || currentTime.isAfter(LocalTime.of(14, 45))) {
            return;
        }

        // Only evaluate when engine is FLAT (no open position and not pending entry)
        if (lifecycleState != TradeLifecycleState.FLAT) {
            return;
        }
        BrokerGateway gateway = brokerGatewayManager.getActiveGateway();
        if (gateway.getOpenPosition().isPresent()) {
            return;
        }

        // Risk check: Can trade today and symbol has not already been traded
        if (!positionSizingEngine.canTradeToday() || tradedSymbolsToday.contains(symbol)) {
            return;
        }

        // Breakout Gate: LTP >= PDH (Previous Day High)
        double pdh = metrics.getPreviousDayHigh();
        if (pdh <= 0 || ltp < pdh) {
            metrics.resetBreakoutTicks();
            lastRadarEvaluationTimeMs.remove(symbol);
            return;
        }

        // 2-Tick Confirmation Rule (prevents 1-tick stop-run fakeouts)
        int consecutiveTicks = metrics.incrementBreakoutTicks();
        if (consecutiveTicks < 2) {
            return;
        }

        // Cooldown & Noise Suppression:
        // - Tick 2 is the initial breakout event: triggers evaluation cycle immediately and logs at INFO once.
        // - Subsequent ticks while continuing to trade above PDH are throttled:
        //   evaluated at most once every 30 seconds per symbol (and logged at DEBUG) to prevent CPU thrashing & log dumps.
        long nowMs = System.currentTimeMillis();
        Long lastSymbolEval = lastRadarEvaluationTimeMs.get(symbol);
        boolean isInitialBreakout = (consecutiveTicks == 2 || lastSymbolEval == null);

        if (!isInitialBreakout) {
            if ((nowMs - lastSymbolEval) < 30_000) {
                return;
            }
        }

        // Global debounce: Prevent launching multiple 210-stock scoring cycles within 1000ms
        long lastGlobal = lastGlobalRadarCycleMs.get();
        if ((nowMs - lastGlobal) < 1000) {
            return;
        }

        // Prevent concurrent overlapping evaluations while a cycle is executing
        if (evaluatingTrigger.compareAndSet(false, true)) {
            lastRadarEvaluationTimeMs.put(symbol, nowMs);
            lastGlobalRadarCycleMs.set(nowMs);
            Thread.ofVirtual().name("breakout-radar-" + symbol).start(() -> {
                try {
                    if (isInitialBreakout) {
                        log.info("⚡ BREAKOUT CONFIRMED for {}: LTP=₹{} >= PDH=₹{} (2-tick gate passed). Running evaluation cycle...",
                            symbol, ltp, pdh);
                    } else {
                        log.debug("Breakout re-evaluation for {} after cooldown: LTP=₹{} >= PDH=₹{}", symbol, ltp, pdh);
                    }
                    evaluateContinuousCycle();
                } catch (Throwable t) {
                    log.error("Real-time breakout evaluation failed for {}: {}", symbol, t.getMessage(), t);
                } finally {
                    evaluatingTrigger.set(false);
                }
            });
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
                    // Place protective stop-loss for the confirmed filled quantity
                    double tickSize = getSymbolTickSize(symbol);
                    double triggerPrice = TickMath.floorToTick(plannedStopPrice, tickSize);
                    double slLimitPrice = TickMath.floorToTick(triggerPrice * 0.9985, tickSize);
                    try {
                        TradeOrder slOrder = gateway.placeOrder(symbol, "SELL", pos.quantity(), slLimitPrice, triggerPrice, "SL");
                        activeSlOrderId = slOrder.orderId();
                        currentArmedStopPrice = triggerPrice;
                        lifecycleState = TradeLifecycleState.ACTIVE_POSITION; // ONLY transition once stop order is placed!
                        strategyHighWaterMark = Math.max(pos.entryPrice(), ltp);
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
                        log.error("CRITICAL: Failed to arm protective stop for filled position {}: {}. Will retry on next tick.", symbol, t.getMessage(), t);
                        activityFeedService.record(
                            "ALERT",
                            "ERROR",
                            gateway.getExecutionMode().name(),
                            activeTradeId,
                            "Emergency: Stop-Loss Failed",
                            "Failed to place protective SL for " + symbol + ": " + t.getMessage() + ". Retrying immediately.",
                            symbol
                        );
                        // Lifecycle state deliberately remains PENDING_ENTRY so the next tick retries arming the stop!
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

                // Dynamic 2-Stage Breakeven & Progressive Trailing
                if (activeSlOrderId != null) {
                    double tickSize = getSymbolTickSize(symbol);
                    double entryPrice = pos.entryPrice();
                    double candidateStop = currentArmedStopPrice;
                    String ratchetReason = null;

                    // Stage 1: Exact Solvency Breakeven Lock (+1.0% gain milestone)
                    // Solves for exact net P&L >= +₹10.00 after all statutory taxes, fees, and Zerodha brokerage
                    if (strategyHighWaterMark >= entryPrice * (1.0 + breakevenTriggerPct / 100.0)) {
                        Optional<Double> beOpt = ChargesBreakdown.calculateExactBreakevenPrice(pos.quantity(), entryPrice, tickSize);
                        if (beOpt.isPresent()) {
                            double breakevenTarget = beOpt.get();
                            if (breakevenTarget > candidateStop) {
                                candidateStop = breakevenTarget;
                                ratchetReason = "Breakeven Lock (+₹10 Net Profit Secured)";
                            }
                        } else {
                            log.warn("Fail-closed: Breakeven calculation did not converge for {} x {} @ entry ₹{}. Retaining existing stop.",
                                pos.quantity(), symbol, entryPrice);
                        }
                    }

                    // Stage 2: Progressive Trailing Buffer (Active ONLY once gain reaches >= 1.5%)
                    if (strategyHighWaterMark >= entryPrice * 1.015) {
                        double trailTarget = TickMath.floorToTick(strategyHighWaterMark * (1.0 - trailingBufferPct / 100.0), tickSize);
                        if (trailTarget >= candidateStop + (3 * tickSize)) {
                            candidateStop = trailTarget;
                            ratchetReason = "Trailing Stop Ratcheted (" + trailingBufferPct + "% Buffer)";
                        }
                    }

                    // Ratchet stop strictly upward if higher than current armed stop and above initial stop
                    if (candidateStop > currentArmedStopPrice && candidateStop > plannedStopPrice) {
                        // Price Inversion Guard: Candidate stop trigger must strictly be below current LTP
                        if (candidateStop >= ltp) {
                            log.warn("PRICE INVERSION GUARD: Candidate stop ₹{} >= current LTP ₹{}. Holding stop modification for {}.",
                                candidateStop, ltp, symbol);
                        } else {
                            double limitPrice = TickMath.floorToTick(candidateStop * 0.9985, tickSize);
                            log.info("RATCHETING STOP for {}: Persistent HWM=₹{}, New Trigger=₹{}, Limit=₹{}, Reason={}",
                                symbol, strategyHighWaterMark, candidateStop, limitPrice, ratchetReason);
                            gateway.modifyOrder(activeSlOrderId, limitPrice, candidateStop);
                            currentArmedStopPrice = candidateStop; // Maintain local confirmed trigger to prevent repetitive modifications
                            activityFeedService.record(
                                "ORDERS",
                                "SUCCESS",
                                gateway.getExecutionMode().name(),
                                activeTradeId,
                                ratchetReason != null ? ratchetReason : "Trailing Stop Ratcheted",
                                "Persistent HWM updated to ₹" + strategyHighWaterMark + ". Stop raised to ₹" + candidateStop + " (Limit: ₹" + limitPrice + ")",
                                symbol,
                                Map.of("highWaterMark", strategyHighWaterMark, "triggerPrice", candidateStop, "limitPrice", limitPrice)
                            );
                        }
                    }
                }
            } else {
                // Position closed (stop-loss was hit or manually closed)
                log.info("Position for {} is no longer open at broker. Trade completed.", symbol);
                if (currentArmedStopPrice > 0 && ltp <= currentArmedStopPrice * 1.002) {
                    positionSizingEngine.recordTradeLoss();
                }
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
        evaluateCycle(LocalTime.of(9, 30));
    }

    @Override
    public synchronized void evaluateContinuousCycle() {
        LocalTime evalTime = lastTickTime != null ? lastTickTime : LocalTime.now(ZoneId.of("Asia/Kolkata"));
        evaluateCycle(evalTime);
    }

    public synchronized void evaluateCycle(LocalTime evaluationTime) {
        ensureSessionCurrent();

        // Step 1: Score all candidates (atomically captures snapshot per candidate) excluding symbols already traded today
        List<ScoredCandidate> scoredList = metricsMap.values().stream()
            .filter(m -> !tradedSymbolsToday.contains(m.getSymbol()))
            .map(m -> scoringEngine.evaluate(m, evaluationTime))
            .toList();

        // Step 2: Rank candidates with tie-breaker and update telemetry leaderboard
        List<ScoredCandidate> ranked = scoringEngine.rankCandidates(scoredList);
        latestTopCandidates.clear();
        latestTopCandidates.addAll(ranked);

        // Broadcast real-time leaderboard update to active UI clients
        if (broadcastServiceProvider != null && broadcastServiceProvider.getIfAvailable() != null) {
            broadcastServiceProvider.getIfAvailable().broadcastLeaderboardUpdate(ranked);
        }

        // Safety Gate: Ensure no position is already open and daily limit is not reached
        BrokerGateway gateway = brokerGatewayManager.getActiveGateway();
        if (gateway.getOpenPosition().isPresent() || lifecycleState != TradeLifecycleState.FLAT) {
            return;
        }

        if (!positionSizingEngine.canTradeToday()) {
            return;
        }

        // Step 3: Check qualification threshold (Score >= 75 and passes hard gates)
        if (ranked.isEmpty() || !ranked.get(0).isQualified()) {
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
        double tickSize = winnerMetrics.getTickSize();

        // Structural Support Filter: Check that Day's Session Low is valid and within allowable distance
        if (effLow15 <= 0 || effLow15 >= entryPrice) {
            log.warn("Trade aborted for {}: Invalid structural support baseline (Low=₹{}, Entry=₹{})",
                winner.symbol(), effLow15, entryPrice);
            return;
        }

        double supportDistPct = ((entryPrice - effLow15) / entryPrice) * 100.0;
        if (supportDistPct > maxSupportDistancePct) {
            log.info("Trade rejected for {}: Support distance {:.2f}% > {:.2f}% cap. Breakout overextended.",
                winner.symbol(), supportDistPct, maxSupportDistancePct);
            activityFeedService.record(
                "RISK",
                "INFO",
                "Breakout Overextended",
                "Rejected " + winner.symbol() + ": Structural support is " + String.format("%.2f", supportDistPct) + "% away (> " + maxSupportDistancePct + "% limit).",
                winner.symbol()
            );
            return;
        }

        // Dynamic stop: Structural support 1 tick below session low, clamped to max allowed stop (0.8% distance) and noise floor
        double maxAllowedStop = TickMath.floorToTick(entryPrice * 0.992, tickSize); // 0.8% planned stop boundary
        double calculatedStop = (supportDistPct < 0.25)
            ? TickMath.floorToTick(entryPrice * 0.9965, tickSize) // Noise floor clamp (0.35%)
            : TickMath.floorToTick(effLow15 - tickSize, tickSize); // 1 tick below support

        double initialStop = Math.max(calculatedStop, maxAllowedStop);
        initialStop = TickMath.floorToTick(initialStop, tickSize);

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

        // Lock symbol so it cannot be traded again today
        tradedSymbolsToday.add(winner.symbol());

        activityFeedService.record(
            "RISK",
            "INFO",
            "Position Sizing Computed",
            "Calculated size: " + quantity + " shares. Entry ~₹" + entryPrice + ", Initial Stop: ₹" + initialStop,
            winner.symbol(),
            Map.of("quantity", quantity, "entryPrice", entryPrice, "initialStop", initialStop)
        );

        // Step 5: Place Marketable Limit Buy Order
        String tradeId = "AUTO-" + LocalDate.now(ZoneId.of("Asia/Kolkata")) + "-" + winner.symbol();
        activeTradeId = tradeId;
        activeSymbol = winner.symbol();
        plannedStopPrice = initialStop;
        plannedQuantity = quantity;
        lifecycleState = TradeLifecycleState.PENDING_ENTRY;

        double marketableBuyLimit = TickMath.ceilToTick(entryPrice * 1.0005, tickSize); // LTP + 0.05% ceil to valid tick
        log.info("DISPATCHING ENTRY ORDER: BUY {} x {} @ Marketable Limit ₹{}", quantity, winner.symbol(), marketableBuyLimit);

        TradeOrder buyOrder = gateway.placeOrder(winner.symbol(), "BUY", quantity, marketableBuyLimit, 0.0, "LIMIT");
        activeEntryOrderId = buyOrder.orderId();
        positionSizingEngine.recordTradeDispatched();
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
            Position pos = immediatePos.get();
            strategyHighWaterMark = Math.max(entryPrice, pos.currentPrice());
            double slLimitPrice = TickMath.floorToTick(initialStop * 0.9985, tickSize);
            try {
                TradeOrder slOrder = gateway.placeOrder(winner.symbol(), "SELL", pos.quantity(), slLimitPrice, initialStop, "SL");
                activeSlOrderId = slOrder.orderId();
                currentArmedStopPrice = initialStop;
                lifecycleState = TradeLifecycleState.ACTIVE_POSITION;
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
            } catch (Throwable t) {
                log.warn("Immediate SL placement failed: {}. Will retry on incoming tick stream.", t.getMessage());
            }
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
        if (activeEntryOrderId != null) {
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
        }

        // 2. Cancel resting stop-loss order if present
        if (activeSlOrderId != null) {
            try {
                gateway.cancelOrder(activeSlOrderId);
            } catch (Throwable t) {
                log.warn("Failed to cancel resting SL order {}: {}", activeSlOrderId, t.getMessage());
            }
            activeSlOrderId = null;
        }

        // 3. Reconcile Position: Check if ANY shares were filled (full or partial fill)
        Optional<Position> optPos = gateway.getOpenPosition();
        if (optPos.isPresent() && optPos.get().quantity() > 0) {
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
            double tickSize = getSymbolTickSize(pos.symbol());
            double aggressiveSellPrice = TickMath.floorToTick(pos.currentPrice() * 0.9990, tickSize);
            gateway.placeOrder(pos.symbol(), "SELL", pos.quantity(), aggressiveSellPrice, 0.0, "LIMIT");

            // Check if immediately flat (e.g. Paper Trading)
            Optional<Position> postExit = gateway.getOpenPosition();
            if (postExit.isEmpty()) {
                lifecycleState = TradeLifecycleState.FLAT;
                activeSymbol = null;
                activeTradeId = null;
                strategyHighWaterMark = 0.0;
                currentArmedStopPrice = 0.0;
            }
        } else {
            log.info("Square-off complete: No open position found.");
            lifecycleState = TradeLifecycleState.FLAT;
            activeSymbol = null;
            activeTradeId = null;
            strategyHighWaterMark = 0.0;
            currentArmedStopPrice = 0.0;
            activityFeedService.record(
                "LIFECYCLE",
                "INFO",
                gateway.getExecutionMode().name(),
                activeTradeId,
                "15:05 Square-Off Check",
                "Square-off completed: Confirmed flat with zero open positions.",
                activeSymbol,
                Map.of()
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

    @Override
    public synchronized Map<String, Object> executeManualEntry(String rawSymbol, Integer customQuantity, Double customStopPrice) {
        return executeManualEntry(rawSymbol, customQuantity, customStopPrice, null);
    }

    @Override
    public synchronized Map<String, Object> executeManualEntry(String rawSymbol, Integer customQuantity, Double customStopPrice, Double customEntryPrice) {
        ensureSessionCurrent();
        String symbol = rawSymbol.trim().toUpperCase().replace("NSE:", "");
        BrokerGateway gateway = brokerGatewayManager.getActiveGateway();

        // Safety Gate: Ensure no position is already open and engine is flat
        if (gateway.getOpenPosition().isPresent() || lifecycleState != TradeLifecycleState.FLAT) {
            throw new IllegalStateException("Cannot take manual entry: A trade position or order is already actively held!");
        }

        Map<String, Object> quote = gateway.getStockQuote(symbol);
        double entryPrice = (quote != null && quote.get("lastPrice") instanceof Number)
            ? ((Number) quote.get("lastPrice")).doubleValue()
            : 0.0;

        // Fallback to ZerodhaLiveGateway directly if active gateway returned 0.0
        if (entryPrice <= 0.0) {
            try {
                Map<String, Object> liveQuote = brokerGatewayManager.getZerodhaLiveGateway().getStockQuote(symbol);
                if (liveQuote != null && liveQuote.get("lastPrice") instanceof Number) {
                    entryPrice = ((Number) liveQuote.get("lastPrice")).doubleValue();
                }
            } catch (Throwable ignored) {}
        }

        // Fallback to client-provided entry price (e.g. from UI modal) if still 0.0
        if (entryPrice <= 0.0 && customEntryPrice != null && customEntryPrice > 0.0) {
            entryPrice = customEntryPrice;
        }

        if (entryPrice <= 0.0) {
            throw new IllegalArgumentException("Unable to fetch live execution price for symbol: " + symbol);
        }

        double tickSize = getSymbolTickSize(symbol);

        // Determine Stop Loss
        double initialStop;
        if (customStopPrice != null && customStopPrice > 0 && customStopPrice < entryPrice) {
            initialStop = TickMath.floorToTick(customStopPrice, tickSize);
        } else {
            // Default 0.65% structural stop below entry
            initialStop = TickMath.floorToTick(entryPrice * 0.9935, tickSize);
        }

        // Determine Quantity
        int quantity;
        if (customQuantity != null && customQuantity > 0) {
            quantity = customQuantity;
        } else {
            quantity = positionSizingEngine.calculateQuantity(symbol, entryPrice, initialStop, gateway);
            if (quantity < 1) {
                throw new IllegalStateException("Risk sizing calculated 0 shares for " + symbol + " (check margin / cash / stop distance)");
            }
        }

        String tradeId = "MAN-" + LocalDate.now(ZoneId.of("Asia/Kolkata")) + "-" + symbol;
        activeTradeId = tradeId;
        activeSymbol = symbol;
        plannedStopPrice = initialStop;
        plannedQuantity = quantity;
        lifecycleState = TradeLifecycleState.PENDING_ENTRY;

        double marketableBuyLimit = TickMath.ceilToTick(entryPrice * 1.0005, tickSize);
        log.info("DISPATCHING MANUAL ENTRY ORDER: BUY {} x {} @ Marketable Limit ₹{}", quantity, symbol, marketableBuyLimit);

        TradeOrder buyOrder = gateway.placeOrder(symbol, "BUY", quantity, marketableBuyLimit, 0.0, "LIMIT");
        activeEntryOrderId = buyOrder.orderId();
        positionSizingEngine.recordTradeDispatched();
        tradedSymbolsToday.add(symbol);

        activityFeedService.record(
            "ORDERS",
            "SUCCESS",
            gateway.getExecutionMode().name(),
            activeTradeId,
            "Manual Entry Dispatched",
            "BUY " + quantity + " x " + symbol + " @ Marketable Limit ₹" + marketableBuyLimit + ". Handed over to Strategy Lifecycle.",
            symbol,
            Map.of("orderId", buyOrder.orderId(), "quantity", quantity, "limitPrice", marketableBuyLimit, "initialStop", initialStop)
        );

        // Immediate fill check (e.g. Paper Trading)
        Optional<Position> immediatePos = gateway.getOpenPosition();
        if (immediatePos.isPresent() && immediatePos.get().symbol().equals(symbol)) {
            Position pos = immediatePos.get();
            strategyHighWaterMark = Math.max(entryPrice, pos.currentPrice());
            double slLimitPrice = TickMath.floorToTick(initialStop * 0.9985, tickSize);
            try {
                TradeOrder slOrder = gateway.placeOrder(symbol, "SELL", pos.quantity(), slLimitPrice, initialStop, "SL");
                activeSlOrderId = slOrder.orderId();
                currentArmedStopPrice = initialStop;
                lifecycleState = TradeLifecycleState.ACTIVE_POSITION;
                log.info("PROTECTIVE SL ENGAGED FOR MANUAL TRADE: OrderId={}, Trigger=₹{}, Limit=₹{}", activeSlOrderId, initialStop, slLimitPrice);
            } catch (Throwable t) {
                log.warn("Immediate SL placement for manual trade failed: {}. Will retry on incoming tick stream.", t.getMessage());
            }
        }

        Map<String, Object> result = new LinkedHashMap<>();
        result.put("success", true);
        result.put("tradeId", tradeId);
        result.put("symbol", symbol);
        result.put("quantity", quantity);
        result.put("entryPrice", entryPrice);
        result.put("initialStop", initialStop);
        result.put("executionMode", gateway.getExecutionMode().name());
        return result;
    }

    @Override
    public Set<String> getTradedSymbolsToday() {
        return Collections.unmodifiableSet(tradedSymbolsToday);
    }
}
