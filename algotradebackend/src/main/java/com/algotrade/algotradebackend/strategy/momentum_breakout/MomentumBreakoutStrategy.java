package com.algotrade.algotradebackend.strategy.momentum_breakout;

import com.algotrade.algotradebackend.broker.BrokerGateway;
import com.algotrade.algotradebackend.broker.BrokerGatewayManager;
import com.algotrade.algotradebackend.core.model.ChargesBreakdown;
import com.algotrade.algotradebackend.core.model.ExecutionMode;
import com.algotrade.algotradebackend.core.model.Position;
import com.algotrade.algotradebackend.core.model.ScoredCandidate;
import com.algotrade.algotradebackend.core.model.TradeOrder;
import com.algotrade.algotradebackend.core.util.TickMath;
import com.algotrade.algotradebackend.risk.PositionSizingEngine;
import com.algotrade.algotradebackend.strategy.TradingStrategy;
import com.algotrade.algotradebackend.core.service.MarketCalendarService;
import com.algotrade.algotradebackend.core.service.EngineActivityFeedService;
import com.algotrade.algotradebackend.persistence.entity.PaperActivePositionEntity;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Service;

import java.time.Instant;
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

    // Multi-Stage Stop Ladder Parameters
    @Value("${algotrade.strategy.ladder.enabled:true}")
    private boolean ladderEnabled = true;

    @Value("${algotrade.strategy.ladder.buffer-pct:0.15}")
    private double ladderBufferPct = 0.15;

    @Value("${algotrade.strategy.ladder.profit-buffer-pct:0.20}")
    private double ladderProfitBufferPct = 0.20;

    @Value("${algotrade.strategy.ladder.inversion-gap-ticks:5}")
    private int ladderInversionGapTicks = 5;

    @Value("${algotrade.strategy.ladder.inversion-gap-pct:0.20}")
    private double ladderInversionGapPct = 0.20;

    @Value("${algotrade.strategy.ladder.incubation-minutes:3}")
    private long ladderIncubationMinutes = 3;

    @Value("${algotrade.strategy.ladder.stage1-trigger-r:0.60}")
    private double ladderStage1TriggerR = 0.60;

    @Value("${algotrade.strategy.ladder.stage1-risk-r:0.35}")
    private double ladderStage1RiskR = 0.35;

    @Value("${algotrade.strategy.ladder.stage2-trigger-r:1.20}")
    private double ladderStage2TriggerR = 1.20;

    @Value("${algotrade.strategy.ladder.stage2-target-net-profit:11.00}")
    private double ladderStage2TargetNetProfit = 11.00;

    @Value("${algotrade.strategy.ladder.stage3-trigger-r:1.50}")
    private double ladderStage3TriggerR = 1.50;

    @Value("${algotrade.strategy.ladder.stagnation-minutes:25}")
    private long ladderStagnationMinutes = 25;

    @Value("${algotrade.strategy.ladder.stagnation-progress-r:0.25}")
    private double ladderStagnationProgressR = 0.25;

    @Value("${algotrade.strategy.ladder.max-duration-minutes:45}")
    private long ladderMaxDurationMinutes = 45;

    @Value("${algotrade.strategy.ladder.throttle-seconds:5}")
    private long ladderThrottleSeconds = 5;

    @Value("${algotrade.strategy.ladder.min-step-ticks:3}")
    private int ladderMinStepTicks = 3;

    @Value("${algotrade.strategy.ladder.mod-budget-tier1:15}")
    private int ladderModBudgetTier1 = 15;

    @Value("${algotrade.strategy.ladder.mod-budget-tier2:20}")
    private int ladderModBudgetTier2 = 20;

    @Value("${algotrade.strategy.ladder.mod-budget-reserve:3}")
    private int ladderModBudgetReserve = 3;

    private final ConcurrentHashMap<String, CandidateMetrics> metricsMap = new ConcurrentHashMap<>();
    private final CopyOnWriteArrayList<ScoredCandidate> latestTopCandidates = new CopyOnWriteArrayList<>();
    private final java.util.Set<String> tradedSymbolsToday = java.util.concurrent.ConcurrentHashMap.newKeySet();

    // Active trade tracking & lifecycle state machine
    private volatile TradeLifecycleState lifecycleState = TradeLifecycleState.FLAT;
    private volatile String activeSymbol = null;
    private volatile String activeEntryOrderId = null;
    private volatile String activeSlOrderId = null;
    private volatile String activeExitOrderId = null;
    private volatile String activeTradeId = null;
    private volatile double strategyHighWaterMark = 0.0;
    private volatile double plannedStopPrice = 0.0;
    private volatile double currentArmedStopPrice = 0.0;
    private volatile double confirmedEntryPrice = 0.0;
    private volatile int plannedQuantity = 0;
    private volatile int currentProtectedQuantity = 0;
    private volatile long entryOrderTimestampMs = 0L;
    private final java.util.concurrent.atomic.AtomicBoolean exitOrderInFlight = new java.util.concurrent.atomic.AtomicBoolean(false);
    private final java.util.concurrent.atomic.AtomicLong lastExitOrderDispatchMs = new java.util.concurrent.atomic.AtomicLong(0);
    private volatile LocalDate lastSessionDate = null;
    private volatile LocalTime lastTickTime = null;

    // Multi-Stage Stop Ladder State (§C4, §C5, §C7, §C9, §C11)
    private volatile int logicalStage = 0;
    private volatile StopLadder.StopOrderPair armedPair = new StopLadder.StopOrderPair(0.0, 0.0);
    private volatile double initialStructuralStop = 0.0; // S0
    private volatile double initialStructuralLimit = 0.0;
    private volatile double unitRisk = 0.0; // 1R = E - S0
    private volatile Instant entryFillTimeUtc = null;
    private volatile Instant hwmTimestampUtc = null;
    private volatile double hwm15MinAgo = 0.0;
    private volatile Instant lastHwmStepTsUtc = null;
    private volatile long lastStopModifyTimeMs = 0L;
    private volatile int slModificationCount = 0;
    private volatile String currentOrderTag = null;

    // Latches (§C7)
    private volatile boolean peak06Latched = false;
    private volatile boolean incubDoneLatched = false;
    private volatile boolean stagDoneLatched = false;
    private volatile boolean t45DoneLatched = false;
    private volatile boolean eodDoneLatched = false;

    // Exit Controller State (§C9)
    private volatile String activeExitPolicy = "NONE"; // NONE, PROTECTED, EMERGENCY
    private volatile int emergencyStep = 0;
    private volatile long lastEmergencyStepTimeMs = 0L;
    private volatile long protectedGraceStartTimeMs = 0L;
    private volatile long emergencyUnfilledStartTimeMs = 0L;

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
    private com.algotrade.algotradebackend.persistence.repository.LiveTradeRepository liveTradeRepository;

    @org.springframework.beans.factory.annotation.Autowired(required = false)
    private com.algotrade.algotradebackend.persistence.repository.PaperActivePositionRepository paperActivePositionRepository;

    @org.springframework.beans.factory.annotation.Autowired(required = false)
    private org.springframework.beans.factory.ObjectProvider<com.algotrade.algotradebackend.websocket.LiveWebSocketBroadcastService> broadcastServiceProvider;

    private final java.util.concurrent.atomic.AtomicBoolean evaluatingTrigger = new java.util.concurrent.atomic.AtomicBoolean(false);
    private final ConcurrentHashMap<String, Long> lastRadarEvaluationTimeMs = new ConcurrentHashMap<>();
    private final ConcurrentHashMap<String, Long> radarSymbolCooldownMs = new ConcurrentHashMap<>();
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
        log.info("Stop Ladder Engine Initialized: enabled={}, bufferPct={}%, profitBufferPct={}%, gap={}t/{}%, incubation={}m, S1(trig=+{}R, soft=-{}R), S2(trig=+{}R, net=₹{}), S3(trig=+{}R), stag={}m, maxDur={}m",
            ladderEnabled, ladderBufferPct, ladderProfitBufferPct, ladderInversionGapTicks, ladderInversionGapPct,
            ladderIncubationMinutes, ladderStage1TriggerR, ladderStage1RiskR, ladderStage2TriggerR, ladderStage2TargetNetProfit,
            ladderStage3TriggerR, ladderStagnationMinutes, ladderMaxDurationMinutes);
        rehydrateActivePositionIfPresent();
    }

    public synchronized void rehydrateActivePositionIfPresent() {
        try {
            BrokerGateway gateway = brokerGatewayManager.getActiveGateway();
            if (gateway == null) return;

            Optional<Position> optPos;
            try {
                optPos = gateway.getOpenPosition();
            } catch (Throwable t) {
                log.error("RECONCILE_PENDING: Broker query failed during startup rehydration. Setting RECONCILE_PENDING; no orders sent. Reason: {}", t.getMessage());
                activityFeedService.record(
                    "ALERT", "ERROR", gateway.getExecutionMode().name(), "RECOVERY",
                    "Reconcile Pending", "Broker query failed during rehydration: " + t.getMessage() + ". No orders sent.", null
                );
                return;
            }

            var dbOpt = (paperActivePositionRepository != null) ? paperActivePositionRepository.findFirstByOrderByEntryTimeDesc() : Optional.<PaperActivePositionEntity>empty();

            if (optPos.isEmpty() || optPos.get().quantity() <= 0) {
                // Broker has no open position
                if (dbOpt.isPresent()) {
                    log.warn("RECOVERY: Local active position found in DB, but broker has 0 net position. Clearing stale DB state.");
                    paperActivePositionRepository.deleteAll();
                }
                setFlatState();
                log.info("RECOVERY: Engine confirmed FLAT at broker.");
                return;
            }

            Position pos = optPos.get();
            Optional<TradeOrder> stopOpt = gateway.getActiveStopOrder(pos.symbol(), null);

            if (dbOpt.isEmpty()) {
                // Unknown position detected at broker with no DB record
                log.error("UNKNOWN OPEN POSITION DETECTED at broker: {} x {} with no active DB record!", pos.quantity(), pos.symbol());
                activityFeedService.record(
                    "ALERT", "WARN", gateway.getExecutionMode().name(), "UNKNOWN-" + pos.symbol(),
                    "Unknown Position Detected", "Broker has " + pos.quantity() + " shares of " + pos.symbol() + " with no active DB record.", pos.symbol()
                );
                this.activeSymbol = pos.symbol();
                this.confirmedEntryPrice = pos.entryPrice();
                this.strategyHighWaterMark = Math.max(pos.entryPrice(), pos.currentPrice());
                this.plannedQuantity = pos.quantity();
                this.currentProtectedQuantity = pos.quantity();
                this.activeTradeId = "UNKNOWN-" + pos.symbol();
                this.entryFillTimeUtc = pos.entryTime() != null ? pos.entryTime() : Instant.now();

                if (stopOpt.isEmpty()) {
                    // No working sell exists: arm emergency SL or EMERGENCY_EXIT
                    double tickSize = getSymbolTickSize(pos.symbol());
                    double emTrigger = TickMath.floorToTick(pos.currentPrice() * 0.992, tickSize);
                    double emLimit = TickMath.floorToTick(emTrigger * (1.0 - ladderBufferPct / 100.0), tickSize);
                    if (StopLadder.isExecutable(emTrigger, pos.currentPrice(), tickSize, ladderInversionGapTicks, ladderInversionGapPct)) {
                        String emTag = generateOrderTag("EM");
                        TradeOrder emOrder = placeOrderWithTagFallback(gateway, pos.symbol(), "SELL", pos.quantity(), emLimit, emTrigger, "SL", emTag);
                        if (emOrder != null) {
                            this.activeSlOrderId = emOrder.orderId();
                            this.armedPair = new StopLadder.StopOrderPair(emTrigger, emLimit);
                            this.currentArmedStopPrice = emTrigger;
                            this.lifecycleState = TradeLifecycleState.ACTIVE_POSITION;
                            log.info("RECOVERY: Armed emergency SL for unknown position: OrderId={}, Trigger=₹{}, Limit=₹{}", activeSlOrderId, emTrigger, emLimit);
                        } else {
                            this.lifecycleState = TradeLifecycleState.PENDING_EXIT;
                            executeEmergencyExit(gateway, pos, "UNKNOWN_POSITION_SL_PLACEMENT_FAILED");
                        }
                    } else {
                        this.lifecycleState = TradeLifecycleState.PENDING_EXIT;
                        executeEmergencyExit(gateway, pos, "UNKNOWN_POSITION_UNEXECUTABLE_SL");
                    }
                } else {
                    TradeOrder existingStop = stopOpt.get();
                    this.activeSlOrderId = existingStop.orderId();
                    this.armedPair = new StopLadder.StopOrderPair(existingStop.triggerPrice(), existingStop.price());
                    this.currentArmedStopPrice = existingStop.triggerPrice();
                    if ("OPEN".equalsIgnoreCase(existingStop.status())) {
                        this.lifecycleState = TradeLifecycleState.PENDING_EXIT;
                        log.warn("RECOVERY: Unknown position has triggered OPEN stop {}. Escalating...", existingStop.orderId());
                        executeEmergencyExit(gateway, pos, "RECOVERY_TRIGGERED_STOP");
                    } else {
                        this.lifecycleState = TradeLifecycleState.ACTIVE_POSITION;
                        log.info("RECOVERY: Reconciled existing stop order {} for unknown position.", existingStop.orderId());
                    }
                }
                return;
            }

            // Normal recovery: Broker has position and DB has trade state
            PaperActivePositionEntity entity = dbOpt.get();
            this.activeSymbol = pos.symbol();
            this.activeTradeId = "RESTORED-" + pos.symbol();
            this.confirmedEntryPrice = entity.getEntryPrice() > 0 ? entity.getEntryPrice() : pos.entryPrice();
            this.initialStructuralStop = entity.getInitialStopLoss() > 0 ? entity.getInitialStopLoss() : (pos.initialStopLoss() > 0 ? pos.initialStopLoss() : confirmedEntryPrice * 0.99);
            this.initialStructuralLimit = entity.getInitialStopLimit() != null && entity.getInitialStopLimit() > 0 ? entity.getInitialStopLimit() : TickMath.floorToTick(initialStructuralStop * (1.0 - ladderBufferPct / 100.0), getSymbolTickSize(pos.symbol()));
            this.unitRisk = entity.getUnitRisk() != null && entity.getUnitRisk() > 0 ? entity.getUnitRisk() : (confirmedEntryPrice - initialStructuralStop);
            this.logicalStage = entity.getLogicalStage();
            this.strategyHighWaterMark = Math.max(entity.getHighWaterMark(), pos.highWaterMark());
            this.entryFillTimeUtc = entity.getEntryTime() != null ? entity.getEntryTime() : pos.entryTime();
            this.hwmTimestampUtc = entity.getHwmTime() != null ? entity.getHwmTime() : Instant.now();
            this.lastHwmStepTsUtc = entity.getLastHwmStepTime();
            this.slModificationCount = entity.getSlModCount();
            this.currentProtectedQuantity = pos.quantity();
            this.plannedQuantity = pos.quantity();

            int mask = entity.getLatchesMask();
            this.peak06Latched = (mask & 1) != 0;
            this.incubDoneLatched = (mask & (1 << 1)) != 0;
            this.stagDoneLatched = (mask & (1 << 2)) != 0;
            this.t45DoneLatched = (mask & (1 << 3)) != 0;
            this.eodDoneLatched = (mask & (1 << 4)) != 0;

            if (stopOpt.isPresent()) {
                TradeOrder slOrder = stopOpt.get();
                this.activeSlOrderId = slOrder.orderId();
                this.armedPair = new StopLadder.StopOrderPair(slOrder.triggerPrice(), slOrder.price());
                this.currentArmedStopPrice = slOrder.triggerPrice();

                if (slOrder.quantity() != pos.quantity()) {
                    log.warn("RECOVERY QUANTITY RESIZE: Broker net qty {} != Stop order qty {}. In-place modifying...", pos.quantity(), slOrder.quantity());
                    try {
                        gateway.modifyOrder(slOrder.orderId(), pos.quantity(), slOrder.price(), slOrder.triggerPrice());
                    } catch (Throwable t) {
                        log.error("Failed to in-place resize stop quantity during recovery: {}", t.getMessage());
                    }
                }

                if ("OPEN".equalsIgnoreCase(slOrder.status())) {
                    this.lifecycleState = TradeLifecycleState.PENDING_EXIT;
                    log.warn("RECOVERY: Found stop order {} in OPEN (triggered) state. Handing off to exit controller.", slOrder.orderId());
                    executeEmergencyExit(gateway, pos, "RECOVERY_TRIGGERED_STOP");
                } else {
                    this.lifecycleState = TradeLifecycleState.ACTIVE_POSITION;
                    log.info("RECOVERY: Successfully restored position: {} x {} @ ₹{} (SL OrderId={}, Trigger=₹{}, Limit=₹{}, Stage={})",
                        pos.quantity(), pos.symbol(), pos.entryPrice(), slOrder.orderId(), armedPair.triggerPrice(), armedPair.limitPrice(), logicalStage);
                }
            } else {
                // Stop absent at broker! Re-arm at persisted armedPair if executable
                log.warn("RECOVERY: No active stop order found at broker for {}! Re-arming at persisted armedPair.", pos.symbol());
                double tickSize = getSymbolTickSize(pos.symbol());
                double trig = entity.getActiveTrailingStop() > 0 ? entity.getActiveTrailingStop() : initialStructuralStop;
                double lim = entity.getArmedLimit() != null && entity.getArmedLimit() > 0 ? entity.getArmedLimit() : TickMath.floorToTick(trig * (1.0 - ladderBufferPct / 100.0), tickSize);
                if (StopLadder.isExecutable(trig, pos.currentPrice(), tickSize, ladderInversionGapTicks, ladderInversionGapPct)) {
                    String rearmTag = generateOrderTag("RA");
                    TradeOrder rearmed = placeOrderWithTagFallback(gateway, pos.symbol(), "SELL", pos.quantity(), lim, trig, "SL", rearmTag);
                    if (rearmed != null) {
                        this.activeSlOrderId = rearmed.orderId();
                        this.armedPair = new StopLadder.StopOrderPair(trig, lim);
                        this.currentArmedStopPrice = trig;
                        this.lifecycleState = TradeLifecycleState.ACTIVE_POSITION;
                        log.info("RECOVERY: Re-armed protective stop at broker: OrderId={}, Trigger=₹{}, Limit=₹{}", activeSlOrderId, trig, lim);
                    } else {
                        this.lifecycleState = TradeLifecycleState.PENDING_EXIT;
                        executeEmergencyExit(gateway, pos, "RECOVERY_REARM_FAILED");
                    }
                } else {
                    this.lifecycleState = TradeLifecycleState.PENDING_EXIT;
                    executeEmergencyExit(gateway, pos, "RECOVERY_UNEXECUTABLE_REARM");
                }
            }

            activityFeedService.record(
                "LIFECYCLE", "INFO", gateway.getExecutionMode().name(), activeTradeId,
                "Active Position Reconciled", "Reconciled active position for " + pos.symbol() + " (" + pos.quantity() + " shares, Stage " + logicalStage + ") across restart.", pos.symbol()
            );
        } catch (Throwable t) {
            log.error("Fatal exception during active position rehydration: {}", t.getMessage(), t);
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
                double ts = m.getTickSize();
                if (ts > 0.0) return ts;
                if (m.getLtp() > 0.0) return TickMath.getExchangeTickSize(m.getLtp());
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
        radarSymbolCooldownMs.clear();
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

    public void updateReferenceData(String symbol, double prevClose, double pdh, double avgVol, double atrPct, double openPrice) {
        CandidateMetrics m = metricsMap.computeIfAbsent(symbol, CandidateMetrics::new);
        m.setReferenceData(prevClose, pdh, avgVol, atrPct, openPrice);
    }

    public void setOfficialOpenPrice(String symbol, double openPrice) {
        if (symbol != null && openPrice > 0) {
            CandidateMetrics m = metricsMap.computeIfAbsent(symbol, CandidateMetrics::new);
            m.setOfficialOpenPrice(openPrice);
        }
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

        // Fast In-Memory Check: Symbol has not already been traded today
        if (tradedSymbolsToday.contains(symbol)) {
            return;
        }

        // Fast In-Memory Check: Breakout Gate: LTP >= PDH + tickSize (Previous Day High breakout)
        double pdh = metrics.getPreviousDayHigh();
        double tickSize = metrics.getTickSize();
        double breakoutThreshold = TickMath.ceilToTick(pdh + tickSize, tickSize);
        if (pdh <= 0 || ltp < breakoutThreshold) {
            metrics.resetBreakoutTicks();
            return;
        }

        // Fast In-Memory Check: 2-Tick Confirmation Rule (prevents 1-tick stop-run fakeouts)
        int consecutiveTicks = metrics.incrementBreakoutTicks();
        if (consecutiveTicks < 2) {
            return;
        }

        // Cooldown & Noise Suppression (Adaptive Micro-Cooldown):
        long nowMs = System.currentTimeMillis();
        Long lastSymbolEval = lastRadarEvaluationTimeMs.get(symbol);
        long cooldownRequired = radarSymbolCooldownMs.getOrDefault(symbol, 3_000L);
        boolean canEvaluate = (lastSymbolEval == null || (nowMs - lastSymbolEval) >= cooldownRequired);

        if (!canEvaluate) {
            return;
        }

        // Gated Checks: ONLY executed once a candidate actually breaks out above PDH with 2-tick confirmation
        if (!positionSizingEngine.canTradeToday()) {
            lastRadarEvaluationTimeMs.put(symbol, nowMs + 30_000L); // 30s cooldown when daily limit reached
            return;
        }

        BrokerGateway gateway = brokerGatewayManager.getActiveGateway();
        if (gateway.getOpenPosition().isPresent()) {
            lastRadarEvaluationTimeMs.put(symbol, nowMs + 10_000L); // 10s cooldown when a position is actively held
            return;
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
            boolean isInitialBreakout = (lastSymbolEval == null || (nowMs - lastSymbolEval) >= 30_000);
            Thread.ofVirtual().name("breakout-radar-" + symbol).start(() -> {
                try {
                    if (isInitialBreakout) {
                        log.info("⚡ BREAKOUT CONFIRMED for {}: LTP=₹{} >= PDH=₹{} (2-tick gate passed). Running evaluation cycle...",
                            symbol, ltp, pdh);
                    } else {
                        log.debug("Breakout re-evaluation for {} after micro-cooldown: LTP=₹{} >= PDH=₹{}", symbol, ltp, pdh);
                    }
                    boolean tradePlaced = evaluateContinuousCycle();
                    if (tradePlaced) {
                        radarSymbolCooldownMs.put(symbol, 86_400_000L); // Traded, locked for the day
                    } else {
                        radarSymbolCooldownMs.put(symbol, 3_000L); // 3s micro-cooldown so subsequent surge is evaluated!
                    }
                } catch (Throwable t) {
                    log.error("Real-time breakout evaluation failed for {}: {}", symbol, t.getMessage(), t);
                    radarSymbolCooldownMs.put(symbol, 3_000L);
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
            long pendingDurationMs = System.currentTimeMillis() - entryOrderTimestampMs;
            if (entryOrderTimestampMs > 0 && pendingDurationMs >= 30_000 && (optPos.isEmpty() || optPos.get().quantity() <= 0)) {
                log.warn("ENTRY TIMEOUT: Order {} for {} unfilled after 30s. Cancelling resting limit order...", activeEntryOrderId, symbol);
                if (activeEntryOrderId != null) {
                    try {
                        gateway.cancelOrder(activeEntryOrderId);
                    } catch (Throwable t) {
                        log.error("Failed to cancel timed-out entry order {}: {}", activeEntryOrderId, t.getMessage());
                    }
                }

                // Authoritative terminal reconciliation: Verify whether broker actually cancelled or if fill raced with cancel
                TradeOrder terminalStatus = (activeEntryOrderId != null) ? gateway.getOrderStatus(activeEntryOrderId) : null;
                Optional<Position> checkPos = gateway.getOpenPosition();

                boolean wasFilled = (terminalStatus != null && "COMPLETE".equalsIgnoreCase(terminalStatus.status()))
                    || (checkPos.isPresent() && checkPos.get().symbol().equals(symbol) && checkPos.get().quantity() > 0);

                if (wasFilled) {
                    int filledQty = checkPos.map(Position::quantity).orElse(plannedQuantity);
                    double filledPrice = (terminalStatus != null && terminalStatus.averagePrice() > 0)
                        ? terminalStatus.averagePrice()
                        : checkPos.map(Position::entryPrice).orElse(confirmedEntryPrice);

                    log.warn("ENTRY TIMEOUT RACE DETECTED: Order {} was FILLED at broker ({} shares @ ₹{}). Arming protective stop-loss!",
                        activeEntryOrderId, filledQty, filledPrice);

                    double tickSize = getSymbolTickSize(symbol);
                    initialStructuralStop = plannedStopPrice;
                    StopLadder.StopOrderPair s0 = StopLadder.stage0Candidate(initialStructuralStop, tickSize, ladderBufferPct / 100.0);
                    initialStructuralLimit = s0.limitPrice();
                    armedPair = s0;
                    logicalStage = 0;
                    unitRisk = filledPrice - initialStructuralStop;
                    entryFillTimeUtc = Instant.now();
                    hwmTimestampUtc = Instant.now();
                    hwm15MinAgo = filledPrice;
                    slModificationCount = 0;
                    String tag = generateOrderTag("SL");
                    currentOrderTag = tag;
                    try {
                        TradeOrder slOrder = placeOrderWithTagFallback(gateway, symbol, "SELL", filledQty, s0.limitPrice(), s0.triggerPrice(), "SL", tag);
                        if (slOrder == null) {
                            throw new IllegalStateException("Gateway returned null stop order for " + symbol);
                        }
                        activeSlOrderId = slOrder.orderId();
                        currentArmedStopPrice = s0.triggerPrice();
                        currentProtectedQuantity = filledQty;
                        confirmedEntryPrice = filledPrice;
                        lifecycleState = TradeLifecycleState.ACTIVE_POSITION;
                        strategyHighWaterMark = Math.max(filledPrice, ltp);
                        log.info("PROTECTIVE SL ARMED AFTER RACE: OrderId={}, Qty={}, Trigger=₹{}, Limit=₹{}, Tag={}",
                            activeSlOrderId, filledQty, s0.triggerPrice(), s0.limitPrice(), tag);
                        Position filledPos = new Position(symbol, filledQty, filledPrice, ltp, initialStructuralStop, s0.triggerPrice(), strategyHighWaterMark, entryFillTimeUtc, gateway.getExecutionMode());
                        persistActivePositionState(gateway, filledPos);
                    } catch (Throwable t) {
                        log.error("CRITICAL: Failed to arm SL after entry timeout race: {}", t.getMessage());
                    }
                    return;
                }

                boolean isCancelled = (terminalStatus != null && ("CANCELLED".equalsIgnoreCase(terminalStatus.status()) || "REJECTED".equalsIgnoreCase(terminalStatus.status())))
                    || (checkPos.isEmpty() || checkPos.get().quantity() <= 0);

                if (isCancelled) {
                    activityFeedService.record(
                        "ORDERS",
                        "WARN",
                        gateway.getExecutionMode().name(),
                        activeTradeId,
                        "Entry Order Timed Out (30s TTL)",
                        "Cancelled unfilled limit buy order for " + symbol + " after 30 seconds. Engine returned to FLAT.",
                        symbol
                    );
                    setFlatState();
                    lastRadarEvaluationTimeMs.put(symbol, System.currentTimeMillis() + 15_000); // 15s debounce
                    return;
                }

                log.warn("Entry order {} cancel status ambiguous. Retrying reconciliation next tick.", activeEntryOrderId);
                return;
            }

            // Waiting for broker to confirm fill of buy order
            if (optPos.isPresent()) {
                Position pos = optPos.get();
                if (pos.symbol().equals(symbol) && pos.quantity() > 0) {
                    // Place protective stop-loss for the confirmed filled quantity
                    double tickSize = getSymbolTickSize(symbol);
                    initialStructuralStop = plannedStopPrice;
                    StopLadder.StopOrderPair s0 = StopLadder.stage0Candidate(initialStructuralStop, tickSize, ladderBufferPct / 100.0);
                    initialStructuralLimit = s0.limitPrice();
                    armedPair = s0;
                    logicalStage = 0;
                    unitRisk = pos.entryPrice() - initialStructuralStop;
                    entryFillTimeUtc = pos.entryTime() != null ? pos.entryTime() : Instant.now();
                    hwmTimestampUtc = Instant.now();
                    hwm15MinAgo = pos.entryPrice();
                    slModificationCount = 0;
                    String tag = generateOrderTag("SL");
                    currentOrderTag = tag;
                    try {
                        TradeOrder slOrder = placeOrderWithTagFallback(gateway, symbol, "SELL", pos.quantity(), s0.limitPrice(), s0.triggerPrice(), "SL", tag);
                        if (slOrder == null) {
                            throw new IllegalStateException("Gateway returned null stop order for " + symbol);
                        }
                        activeSlOrderId = slOrder.orderId();
                        currentArmedStopPrice = s0.triggerPrice();
                        currentProtectedQuantity = pos.quantity();
                        confirmedEntryPrice = pos.entryPrice();
                        lifecycleState = TradeLifecycleState.ACTIVE_POSITION; // ONLY transition once stop order is placed!
                        strategyHighWaterMark = Math.max(pos.entryPrice(), ltp);
                        log.info("PROTECTIVE SL CONFIRMED & ARMED: OrderId={}, Qty={}, Trigger=₹{}, Limit=₹{}, Tag={}",
                            activeSlOrderId, pos.quantity(), s0.triggerPrice(), s0.limitPrice(), tag);
                        persistActivePositionState(gateway, pos);
                        activityFeedService.record(
                            "ORDERS",
                            "SUCCESS",
                            gateway.getExecutionMode().name(),
                            activeTradeId,
                            "Protective Stop-Loss Armed",
                            "Position confirmed (" + pos.quantity() + " shares). SL armed: Trigger=₹" + s0.triggerPrice() + ", Limit=₹" + s0.limitPrice(),
                            symbol,
                            Map.of("slOrderId", activeSlOrderId, "triggerPrice", s0.triggerPrice(), "limitPrice", s0.limitPrice(), "quantity", pos.quantity())
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
                // Partial Fill Protection: If position quantity expanded since initial SL placement, resize the protective stop
                if (activeSlOrderId != null && pos.quantity() > currentProtectedQuantity) {
                    log.warn("PARTIAL FILL EXPANSION: Position quantity ({} shares) > Protected SL quantity ({} shares). Resizing stop order in-place...",
                        pos.quantity(), currentProtectedQuantity);
                    try {
                        double tickSize = getSymbolTickSize(symbol);
                        double triggerPrice = TickMath.floorToTick(currentArmedStopPrice > 0 ? currentArmedStopPrice : plannedStopPrice, tickSize);
                        double slLimitPrice = TickMath.floorToTick(triggerPrice * 0.9985, tickSize);
                        gateway.modifyOrder(activeSlOrderId, pos.quantity(), slLimitPrice, triggerPrice);
                        currentProtectedQuantity = pos.quantity();
                        log.info("PROTECTIVE SL RESIZED (IN-PLACE): OrderId={}, New Qty={}, Trigger=₹{}, Limit=₹{}",
                            activeSlOrderId, pos.quantity(), triggerPrice, slLimitPrice);
                        activityFeedService.record(
                            "ORDERS",
                            "SUCCESS",
                            gateway.getExecutionMode().name(),
                            activeTradeId,
                            "Protective Stop-Loss Resized",
                            "Additional shares filled. Protective stop resized in-place to " + pos.quantity() + " shares @ Trigger ₹" + triggerPrice,
                            symbol
                        );
                    } catch (Throwable t) {
                        log.error("Failed to in-place resize protective stop: {}. Verifying resting order status...", t.getMessage());
                        TradeOrder slStatus = gateway.getOrderStatus(activeSlOrderId);
                        if (slStatus == null || "CANCELLED".equalsIgnoreCase(slStatus.status()) || "REJECTED".equalsIgnoreCase(slStatus.status())) {
                            try {
                                double tickSize = getSymbolTickSize(symbol);
                                double triggerPrice = TickMath.floorToTick(currentArmedStopPrice > 0 ? currentArmedStopPrice : plannedStopPrice, tickSize);
                                double slLimitPrice = TickMath.floorToTick(triggerPrice * 0.9985, tickSize);
                                TradeOrder fallbackSl = gateway.placeOrder(symbol, "SELL", pos.quantity(), slLimitPrice, triggerPrice, "SL");
                                activeSlOrderId = fallbackSl.orderId();
                                currentProtectedQuantity = pos.quantity();
                                log.info("RECOVERY: Replaced failed stop with new SL order {}", activeSlOrderId);
                            } catch (Throwable t2) {
                                log.error("CRITICAL: Failed emergency SL replacement for {}: {}", symbol, t2.getMessage());
                            }
                        }
                    }
                }

                // Check resting Stop order status for triggered-stop watchdog (§C9)
                if (activeSlOrderId != null) {
                    TradeOrder slStatus = gateway.getOrderStatus(activeSlOrderId);
                    if (slStatus != null && "OPEN".equalsIgnoreCase(slStatus.status())) {
                        double bestBid = gateway.getBestBid(symbol).orElse(ltp);
                        double activeLimit = armedPair.limitPrice() > 0 ? armedPair.limitPrice() : (slStatus.price() > 0 ? slStatus.price() : currentArmedStopPrice * (1.0 - ladderBufferPct / 100.0));
                        if (bestBid < activeLimit) {
                            long graceMs = logicalStage >= 2 ? 5000L : 3000L;
                            if (protectedGraceStartTimeMs == 0L) {
                                protectedGraceStartTimeMs = System.currentTimeMillis();
                            } else if (System.currentTimeMillis() - protectedGraceStartTimeMs >= graceMs) {
                                log.warn("TRIGGERED STOP GAP ESCALATION: Order {} OPEN with BestBid ₹{} < Limit ₹{} for > {}ms. Escalating to EMERGENCY_EXIT.",
                                    activeSlOrderId, bestBid, activeLimit, graceMs);
                                executeEmergencyExit(gateway, pos, "TRIGGERED_STOP_GAP_ESCALATION");
                                return;
                            }
                        } else {
                            protectedGraceStartTimeMs = 0L;
                        }
                    }
                }

                // Update persistent high-water mark monotonically (never drops on pullbacks)
                double prevHwm = strategyHighWaterMark;
                strategyHighWaterMark = Math.max(strategyHighWaterMark, ltp);
                Instant nowUtc = Instant.now();
                if (strategyHighWaterMark > prevHwm) {
                    hwmTimestampUtc = nowUtc;
                    lastHwmStepTsUtc = nowUtc;
                }
                if (entryFillTimeUtc == null) {
                    entryFillTimeUtc = pos.entryTime() != null ? pos.entryTime() : nowUtc;
                }
                long elapsedMinutes = java.time.Duration.between(entryFillTimeUtc, nowUtc).toMinutes();
                if (hwm15MinAgo == 0.0 || (lastHwmStepTsUtc != null && java.time.Duration.between(lastHwmStepTsUtc, nowUtc).toMinutes() >= 15)) {
                    hwm15MinAgo = (hwm15MinAgo == 0.0) ? pos.entryPrice() : strategyHighWaterMark;
                }

                // Update Latches (§C7)
                if (!incubDoneLatched && elapsedMinutes >= ladderIncubationMinutes) {
                    incubDoneLatched = true;
                    log.info("LADDER INCUBATION COMPLETED for {} ({} min passed)", symbol, ladderIncubationMinutes);
                }
                double s0 = initialStructuralStop > 0 ? initialStructuralStop : plannedStopPrice;
                double entryPrice = pos.entryPrice();
                double unitRiskVal = unitRisk > 0 ? unitRisk : (entryPrice - s0);
                if (!peak06Latched && (ltp >= entryPrice + (ladderStage1TriggerR * unitRiskVal) || strategyHighWaterMark >= entryPrice + (ladderStage1TriggerR * unitRiskVal))) {
                    peak06Latched = true;
                    log.info("LADDER PEAK06 LATCHED for {}: LTP ₹{} reached +{}R", symbol, ltp, ladderStage1TriggerR);
                }

                double tickSize = getSymbolTickSize(symbol);
                double bufferPct = ladderBufferPct / 100.0;
                double profitBufferPct = ladderProfitBufferPct / 100.0;

                // Time Supervisor Checks (§C7)
                // 1. Stagnation Check at 25 min (one-shot latch)
                if (elapsedMinutes >= ladderStagnationMinutes && !stagDoneLatched) {
                    stagDoneLatched = true;
                    boolean isStag = StopLadder.isStagnant(elapsedMinutes, strategyHighWaterMark, hwm15MinAgo, unitRiskVal, logicalStage, ladderStagnationMinutes, ladderStagnationProgressR);
                    if (isStag) {
                        log.warn("LADDER STAGNATION DETECTED for {} at {} min: HWM=₹{}, HWM15m=₹{}, LogicalStage={}",
                            symbol, elapsedMinutes, strategyHighWaterMark, hwm15MinAgo, logicalStage);
                        if (logicalStage >= 2) {
                            // Branch (a): Stage 2 or 3 already confirmed -> nothing
                            log.info("Stagnation branch (a): Stage {} already confirmed. No action.", logicalStage);
                        } else {
                            double bestBid = gateway.getBestBid(symbol).orElse(ltp);
                            Optional<StopLadder.StopOrderPair> s2Opt = StopLadder.stage2Candidate(pos.quantity(), entryPrice, tickSize, profitBufferPct, ladderStage2TargetNetProfit);
                            if (s2Opt.isPresent() && bestBid >= s2Opt.get().limitPrice()) {
                                // Branch (b): bid >= L2 -> Arm Stage 2 or run PROTECTED exit
                                StopLadder.StopOrderPair s2Pair = s2Opt.get();
                                if (StopLadder.isExecutable(s2Pair.triggerPrice(), ltp, tickSize, ladderInversionGapTicks, ladderInversionGapPct)) {
                                    gateway.modifyOrder(activeSlOrderId, s2Pair.limitPrice(), s2Pair.triggerPrice());
                                    armedPair = armedPair.merge(s2Pair);
                                    logicalStage = Math.max(logicalStage, 2);
                                    currentArmedStopPrice = armedPair.triggerPrice();
                                    slModificationCount++;
                                    lastStopModifyTimeMs = System.currentTimeMillis();
                                    persistActivePositionState(gateway, pos);
                                } else {
                                    executeProtectedExit(gateway, pos, s2Pair.limitPrice());
                                    return;
                                }
                            } else {
                                StopLadder.StopOrderPair s1Pair = StopLadder.stage1Candidate(entryPrice, s0, tickSize, bufferPct, ladderStage1RiskR);
                                double gap = StopLadder.calculateInversionGap(ltp, tickSize, ladderInversionGapTicks, ladderInversionGapPct);
                                if (ltp >= s1Pair.triggerPrice() + gap) {
                                    // Branch (c): Arm Stage 1
                                    if (logicalStage < 1 && StopLadder.isExecutable(s1Pair.triggerPrice(), ltp, tickSize, ladderInversionGapTicks, ladderInversionGapPct)) {
                                        gateway.modifyOrder(activeSlOrderId, s1Pair.limitPrice(), s1Pair.triggerPrice());
                                        armedPair = armedPair.merge(s1Pair);
                                        logicalStage = Math.max(logicalStage, 1);
                                        currentArmedStopPrice = armedPair.triggerPrice();
                                        slModificationCount++;
                                        lastStopModifyTimeMs = System.currentTimeMillis();
                                        persistActivePositionState(gateway, pos);
                                    }
                                } else {
                                    // Branch (d): ltp < T1 + gap -> Controlled EMERGENCY exit
                                    executeEmergencyExit(gateway, pos, "STAGNATION_CONTROLLED_EXIT");
                                    return;
                                }
                            }
                        }
                    }
                }

                // 2. Max Duration Check at 45 min (one-shot latch)
                if (elapsedMinutes >= ladderMaxDurationMinutes && !t45DoneLatched) {
                    t45DoneLatched = true;
                    if (logicalStage <= 2) {
                        log.warn("45-MIN TIME SUPERVISOR: Trade duration {}m >= {}m with Stage {}. Mandatory close.", elapsedMinutes, ladderMaxDurationMinutes, logicalStage);
                        if (logicalStage == 2) {
                            executeProtectedExit(gateway, pos, armedPair.limitPrice());
                        } else {
                            executeEmergencyExit(gateway, pos, "45_MIN_MAX_DURATION_EXCEEDED");
                        }
                        return;
                    } else {
                        log.info("45-MIN TIME SUPERVISOR: Trade duration {}m with Stage 3 runner. Holding position until trail breached or 15:05.", elapsedMinutes);
                    }
                }

                // Opportunistic Price Ratchet (§C6)
                if (activeSlOrderId != null && ladderEnabled) {
                    StopLadder.StopOrderPair s0Cand = StopLadder.stage0Candidate(s0, tickSize, bufferPct);
                    StopLadder.StopOrderPair s1Cand = (incubDoneLatched && peak06Latched) ? StopLadder.stage1Candidate(entryPrice, s0, tickSize, bufferPct, ladderStage1RiskR) : null;
                    Optional<StopLadder.StopOrderPair> s2CandOpt = (strategyHighWaterMark >= entryPrice + (ladderStage2TriggerR * unitRiskVal))
                        ? StopLadder.stage2Candidate(pos.quantity(), entryPrice, tickSize, profitBufferPct, ladderStage2TargetNetProfit)
                        : Optional.empty();
                    StopLadder.StopOrderPair s3Cand = ((logicalStage >= 2 || s2CandOpt.isPresent()) && strategyHighWaterMark >= entryPrice + (ladderStage3TriggerR * unitRiskVal))
                        ? StopLadder.stage3Candidate(entryPrice, strategyHighWaterMark, tickSize, profitBufferPct, bufferPct, Optional.empty(), Optional.empty())
                        : null;

                    StopLadder.StopOrderPair dominant = s0Cand;
                    int targetStage = 0;
                    String ratchetReason = null;
                    if (s1Cand != null) {
                        dominant = dominant.merge(s1Cand);
                        targetStage = Math.max(targetStage, 1);
                        ratchetReason = "Stage 1 Soft Stop (-" + ladderStage1RiskR + "R)";
                    }
                    if (s2CandOpt.isPresent()) {
                        dominant = dominant.merge(s2CandOpt.get());
                        targetStage = Math.max(targetStage, 2);
                        ratchetReason = "Stage 2 Floor Locked (₹" + ladderStage2TargetNetProfit + " Net Target)";
                    }
                    if (s3Cand != null) {
                        dominant = dominant.merge(s3Cand);
                        targetStage = Math.max(targetStage, 3);
                        ratchetReason = "Stage 3 Dynamic Runner Trail";
                    }

                    StopLadder.StopOrderPair proposedPair = armedPair.merge(dominant);

                    // Throttle, step size, and modification budget guards (§C5)
                    double triggerStep = proposedPair.triggerPrice() - armedPair.triggerPrice();
                    double minStep = ladderMinStepTicks * tickSize;
                    if (slModificationCount >= ladderModBudgetTier1) {
                        minStep = Math.max(minStep, 0.25 * unitRiskVal);
                    }
                    boolean budgetOk = (slModificationCount < 20 || targetStage > logicalStage) && (slModificationCount + ladderModBudgetReserve < 25);
                    boolean throttleOk = (System.currentTimeMillis() - lastStopModifyTimeMs >= ladderThrottleSeconds * 1000L);
                    boolean executable = StopLadder.isExecutable(proposedPair.triggerPrice(), ltp, tickSize, ladderInversionGapTicks, ladderInversionGapPct);

                    if (triggerStep >= minStep && budgetOk && throttleOk && executable) {
                        log.info("RATCHETING STOP (Stage {} -> {}): HWM=₹{}, New Trigger=₹{}, Limit=₹{}, Reason={}",
                            logicalStage, targetStage, strategyHighWaterMark, proposedPair.triggerPrice(), proposedPair.limitPrice(), ratchetReason);
                        try {
                            gateway.modifyOrder(activeSlOrderId, proposedPair.limitPrice(), proposedPair.triggerPrice());
                            armedPair = proposedPair;
                            logicalStage = Math.max(logicalStage, targetStage);
                            currentArmedStopPrice = proposedPair.triggerPrice();
                            slModificationCount++;
                            lastStopModifyTimeMs = System.currentTimeMillis();
                            persistActivePositionState(gateway, pos);
                            activityFeedService.record(
                                "ORDERS",
                                "SUCCESS",
                                gateway.getExecutionMode().name(),
                                activeTradeId,
                                ratchetReason != null ? ratchetReason : "Stop Ratcheted",
                                "Raised stop to Trigger ₹" + proposedPair.triggerPrice() + " / Limit ₹" + proposedPair.limitPrice() + " (Stage " + logicalStage + ")",
                                symbol,
                                Map.of("triggerPrice", proposedPair.triggerPrice(), "limitPrice", proposedPair.limitPrice(), "logicalStage", logicalStage, "highWaterMark", strategyHighWaterMark)
                            );
                        } catch (Throwable t) {
                            log.error("Failed to ratchet stop order: {}", t.getMessage());
                        }
                    }
                }
            } else {
                // Position closed (stop-loss was hit or closed at broker)
                log.info("Position for {} is no longer open at broker. Trade completed.", symbol);
                TradeOrder slStatus = (activeSlOrderId != null) ? gateway.getOrderStatus(activeSlOrderId) : null;
                double executedPrice = (slStatus != null && slStatus.averagePrice() > 0)
                    ? slStatus.averagePrice()
                    : (currentArmedStopPrice > 0 ? currentArmedStopPrice : ltp);
                handleTradeCompleted(gateway, symbol, currentProtectedQuantity > 0 ? currentProtectedQuantity : plannedQuantity, confirmedEntryPrice, executedPrice, "STOP_LOSS_HIT");
            }
            return;
        }

        if (lifecycleState == TradeLifecycleState.PENDING_EXIT) {
            // Waiting for square-off exit or manual exit to confirm flat
            if (optPos.isEmpty() || (optPos.get().quantity() <= 0)) {
                log.info("Exit confirmed flat at broker for {}.", symbol);
                TradeOrder exitStatus = (activeExitOrderId != null) ? gateway.getOrderStatus(activeExitOrderId) : null;
                double executedPrice = (exitStatus != null && exitStatus.averagePrice() > 0)
                    ? exitStatus.averagePrice()
                    : ltp;
                handleTradeCompleted(gateway, symbol, currentProtectedQuantity > 0 ? currentProtectedQuantity : plannedQuantity, confirmedEntryPrice, executedPrice, "EXIT_CONFIRMED");
            } else if (now.isAfter(LocalTime.of(15, 6))) {
                long nowMs = System.currentTimeMillis();
                if (exitOrderInFlight.get()) {
                    if (nowMs - lastExitOrderDispatchMs.get() >= 3000) {
                        TradeOrder status = (activeExitOrderId != null) ? gateway.getOrderStatus(activeExitOrderId) : null;
                        if (status != null && ("REJECTED".equalsIgnoreCase(status.status()) || "CANCELLED".equalsIgnoreCase(status.status()))) {
                            log.warn("Exit order {} was {}. Resetting flight latch for emergency retry.", activeExitOrderId, status.status());
                            exitOrderInFlight.set(false);
                        } else {
                            Position pos = optPos.get();
                            executeEmergencyExit(gateway, pos, "POST_15_06_LADDER_ESCALATION");
                            lastExitOrderDispatchMs.set(nowMs);
                        }
                    }
                } else if (exitOrderInFlight.compareAndSet(false, true)) {
                    lastExitOrderDispatchMs.set(nowMs);
                    Position pos = optPos.get();
                    log.warn("POST-15:05 ESCALATION: Dispatching emergency exit ladder for {} ({} shares)", pos.symbol(), pos.quantity());
                    try {
                        executeEmergencyExit(gateway, pos, "POST_15_05_ESCALATION");
                    } catch (Throwable t) {
                        log.error("Emergency exit ladder failed: {}", t.getMessage());
                        exitOrderInFlight.set(false);
                    }
                }
            }
        }
    }

    @Override
    public synchronized void evaluate0930() {
        evaluateCycle(LocalTime.of(9, 30));
    }

    @Override
    public synchronized boolean evaluateContinuousCycle() {
        LocalTime evalTime = lastTickTime != null ? lastTickTime : LocalTime.now(ZoneId.of("Asia/Kolkata"));
        return evaluateCycle(evalTime);
    }

    public synchronized boolean evaluateCycle(LocalTime evaluationTime) {
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
            return false;
        }

        if (!positionSizingEngine.canTradeToday()) {
            return false;
        }

        boolean orderDispatched = false;

        // Step 3: Iterate candidates in rank order (Runner-Up Evaluation)
        for (ScoredCandidate winner : ranked) {
            if (!winner.isQualified() || winner.totalScore() < 75) {
                break; // Candidates are sorted; none of the remaining meet qualification cutoff
            }
            if (tradedSymbolsToday.contains(winner.symbol())) {
                continue;
            }

            CandidateMetrics winnerMetrics = metricsMap.get(winner.symbol());
            if (winnerMetrics == null) continue;

            double entryPrice = winner.ltp();
            double effLow15 = winnerMetrics.getLow15();
            double tickSize = winnerMetrics.getTickSize();

            // Structural Support: Day's Session Low. Fallback to 0.5% below entry if low is missing or at/above entry
            if (effLow15 <= 0 || effLow15 >= entryPrice) {
                effLow15 = TickMath.floorToTick(entryPrice * 0.995, tickSize);
            }

            double maxAllowedStop = TickMath.floorToTick(entryPrice * 0.992, tickSize); // 0.8% planned stop boundary
            double noiseFloorStop = TickMath.floorToTick(entryPrice * 0.9955, tickSize); // 0.45% noise floor boundary

            double supportDistPct = ((entryPrice - effLow15) / entryPrice) * 100.0;
            if (supportDistPct > maxSupportDistancePct) {
                log.info("Candidate {} session low is {}% away (> {}% cap). Clamping initial stop to max allowed boundary (0.8%).",
                    winner.symbol(), String.format("%.2f", supportDistPct), maxSupportDistancePct);
                effLow15 = maxAllowedStop;
                supportDistPct = 0.8;
            }

            // Dynamic stop: Structural support 1 tick below session low, clamped to max allowed stop (0.8% distance) and noise floor (0.45%)
            double calculatedStop = (supportDistPct < 0.45)
                ? noiseFloorStop
                : TickMath.floorToTick(effLow15 - tickSize, tickSize); // 1 tick below support

            double initialStop = Math.max(calculatedStop, maxAllowedStop);
            initialStop = TickMath.floorToTick(initialStop, tickSize);

            if (initialStop <= 0 || initialStop >= entryPrice) {
                log.warn("Candidate {} skipped: Invalid stop price ₹{} (Entry ~₹{})", winner.symbol(), initialStop, entryPrice);
                continue;
            }

            // Step 4b: Fix N3 - Size against marketable buy limit (worst-case fill price)
            double marketableBuyLimit = TickMath.ceilToTick(entryPrice * 1.0005, tickSize); // LTP + 0.05% ceil to valid tick
            int quantity = positionSizingEngine.calculateQuantity(winner.symbol(), marketableBuyLimit, initialStop, gateway);
            if (quantity < 1) {
                log.debug("Candidate {} skipped: Calculated quantity is 0 shares at buy limit ₹{}. Evaluating runner-up.", winner.symbol(), marketableBuyLimit);
                continue;
            }

            // Lock symbol so it cannot be traded again today
            tradedSymbolsToday.add(winner.symbol());

            activityFeedService.record(
                "STRATEGY",
                "SUCCESS",
                "Breakout Candidate Qualified",
                winner.symbol() + " (Rank " + winner.rank() + ") passed qualification with score " + winner.totalScore() + "/100 >= 75.",
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

            activityFeedService.record(
                "RISK",
                "INFO",
                "Position Sizing Computed",
                "Calculated size: " + quantity + " shares. Entry Limit: ₹" + marketableBuyLimit + " (LTP ~₹" + entryPrice + "), Initial Stop: ₹" + initialStop,
                winner.symbol(),
                Map.of("quantity", quantity, "entryPrice", entryPrice, "limitPrice", marketableBuyLimit, "initialStop", initialStop)
            );

            // Step 5: Place Marketable Limit Buy Order
            String tradeId = "AUTO-" + LocalDate.now(ZoneId.of("Asia/Kolkata")) + "-" + winner.symbol();
            log.info("DISPATCHING ENTRY ORDER: BUY {} x {} @ Marketable Limit ₹{}", quantity, winner.symbol(), marketableBuyLimit);

            TradeOrder buyOrder;
            try {
                buyOrder = gateway.placeOrder(winner.symbol(), "BUY", quantity, marketableBuyLimit, 0.0, "LIMIT");
                activeTradeId = tradeId;
                activeSymbol = winner.symbol();
                plannedStopPrice = initialStop;
                plannedQuantity = quantity;
                activeEntryOrderId = buyOrder.orderId();
                entryOrderTimestampMs = System.currentTimeMillis();
                lifecycleState = TradeLifecycleState.PENDING_ENTRY;
                positionSizingEngine.recordTradeDispatched(gateway.getExecutionMode());
                orderDispatched = true;
            } catch (Throwable t) {
                log.error("CRITICAL: Failed to dispatch entry order for {}: {}. Rolling back state to FLAT.", winner.symbol(), t.getMessage(), t);
                setFlatState();
                lastRadarEvaluationTimeMs.put(winner.symbol(), System.currentTimeMillis() + 60_000); // 1-minute lockout on failure
                activityFeedService.record(
                    "ALERT",
                    "ERROR",
                    gateway.getExecutionMode().name(),
                    tradeId,
                    "Entry Order Failed",
                    "Broker order placement failed for " + winner.symbol() + ": " + t.getMessage(),
                    winner.symbol()
                );
                return false;
            }

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
                initialStructuralStop = initialStop;
                StopLadder.StopOrderPair s0 = StopLadder.stage0Candidate(initialStructuralStop, tickSize, ladderBufferPct / 100.0);
                initialStructuralLimit = s0.limitPrice();
                armedPair = s0;
                logicalStage = 0;
                unitRisk = pos.entryPrice() - initialStructuralStop;
                entryFillTimeUtc = pos.entryTime() != null ? pos.entryTime() : Instant.now();
                hwmTimestampUtc = Instant.now();
                hwm15MinAgo = pos.entryPrice();
                slModificationCount = 0;
                strategyHighWaterMark = Math.max(pos.entryPrice(), pos.currentPrice());
                String tag = generateOrderTag("SL");
                currentOrderTag = tag;
                try {
                    TradeOrder slOrder = placeOrderWithTagFallback(gateway, winner.symbol(), "SELL", pos.quantity(), s0.limitPrice(), s0.triggerPrice(), "SL", tag);
                    if (slOrder == null) {
                        throw new IllegalStateException("Gateway returned null stop order for " + winner.symbol());
                    }
                    activeSlOrderId = slOrder.orderId();
                    currentArmedStopPrice = s0.triggerPrice();
                    currentProtectedQuantity = pos.quantity();
                    confirmedEntryPrice = pos.entryPrice();
                    lifecycleState = TradeLifecycleState.ACTIVE_POSITION;
                    log.info("PROTECTIVE SL ENGAGED: OrderId={}, Trigger=₹{}, Limit=₹{}, Tag={}", activeSlOrderId, s0.triggerPrice(), s0.limitPrice(), tag);
                    persistActivePositionState(gateway, pos);
                    activityFeedService.record(
                        "ORDERS",
                        "SUCCESS",
                        gateway.getExecutionMode().name(),
                        activeTradeId,
                        "Protective Stop-Loss Armed",
                        "Stop-Loss order engaged: Trigger=₹" + s0.triggerPrice() + ", Limit=₹" + s0.limitPrice(),
                        winner.symbol(),
                        Map.of("slOrderId", activeSlOrderId, "triggerPrice", s0.triggerPrice(), "limitPrice", s0.limitPrice(), "quantity", pos.quantity())
                    );
                } catch (Throwable t) {
                    log.warn("Immediate SL placement failed: {}. Will retry on incoming tick stream.", t.getMessage());
                }
            } else {
                log.info("Entry order dispatched for {}. Waiting for fill confirmation before placing protective stop...", winner.symbol());
            }

            break; // Order placed; stop evaluating further candidates
        }
        return orderDispatched;
    }

    @Override
    @Scheduled(cron = "0 5 15 * * MON-FRI", zone = "Asia/Kolkata")
    public synchronized void executeSquareOff() {
        log.warn("=== 15:05:00 MANDATORY INTRADAY SQUARE-OFF ENGAGED ===");
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

        // 2. Square-off pipeline (§C9) - Freeze ratchets and execute PROTECTED or EMERGENCY exit
        eodDoneLatched = true;
        if (activeSlOrderId != null) {
            TradeOrder slStatus = gateway.getOrderStatus(activeSlOrderId);
            if (slStatus != null && "COMPLETE".equalsIgnoreCase(slStatus.status())) {
                log.info("Protective SL order {} was FILLED before 15:05 square-off. Settling via SL fill.", activeSlOrderId);
                double fillPrice = slStatus.averagePrice() > 0 ? slStatus.averagePrice() : (currentArmedStopPrice > 0 ? currentArmedStopPrice : (confirmedEntryPrice > 0 ? confirmedEntryPrice : 0.0));
                handleTradeCompleted(gateway, activeSymbol != null ? activeSymbol : "", currentProtectedQuantity, confirmedEntryPrice, fillPrice, "STOP_LOSS_HIT_AT_EOD");
                return;
            }
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
                "Closing active position " + pos.symbol() + " (" + pos.quantity() + " shares) ahead of broker cutoffs.",
                pos.symbol(),
                Map.of("quantity", pos.quantity(), "entryPrice", pos.entryPrice(), "currentPrice", pos.currentPrice())
            );

            lifecycleState = TradeLifecycleState.PENDING_EXIT;
            double bestBid = gateway.getBestBid(pos.symbol()).orElse(pos.currentPrice());
            if (logicalStage >= 2 && bestBid >= armedPair.limitPrice()) {
                log.info("15:05 SQUARE-OFF: Running PROTECTED_EXIT at floor ₹{} (Bid ₹{})", armedPair.limitPrice(), bestBid);
                executeProtectedExit(gateway, pos, armedPair.limitPrice());
            } else {
                log.warn("15:05 SQUARE-OFF: Running EMERGENCY_EXIT ladder (BestBid=₹{}, Stage={})", bestBid, logicalStage);
                executeEmergencyExit(gateway, pos, "15:05_MANDATORY_SQUARE_OFF");
            }

            // 4. Active Reconciliation Loop: Verify flat within 5 seconds and retrieve fill price
            double executedPrice = 0.0;
            for (int attempt = 0; attempt < 10; attempt++) {
                try {
                    Thread.sleep(500);
                } catch (InterruptedException ignored) {}

                TradeOrder exitStatus = (activeExitOrderId != null) ? gateway.getOrderStatus(activeExitOrderId) : null;
                if (exitStatus != null && exitStatus.averagePrice() > 0) {
                    executedPrice = exitStatus.averagePrice();
                }

                Optional<Position> checkPos = gateway.getOpenPosition();
                if (checkPos.isEmpty() || checkPos.get().quantity() <= 0) {
                    if (executedPrice <= 0) {
                        executedPrice = (exitStatus != null && exitStatus.price() > 0) ? exitStatus.price() : pos.currentPrice();
                    }
                    handleTradeCompleted(gateway, pos.symbol(), pos.quantity(), pos.entryPrice(), executedPrice, "MANDATORY_SQUARE_OFF_1505");
                    log.info("15:05 Square-off confirmed flat and settled on attempt {}.", attempt + 1);
                    return;
                }
            }

            // Fallback timeout exit settlement
            if (executedPrice <= 0) executedPrice = pos.currentPrice();
            handleTradeCompleted(gateway, pos.symbol(), pos.quantity(), pos.entryPrice(), executedPrice, "MANDATORY_SQUARE_OFF_TIMEOUT");
        } else {
            log.info("Square-off complete: No open position found.");
            setFlatState();
            broadcastOverviewIfAvailable();
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

    private synchronized void handleTradeCompleted(
        BrokerGateway gateway,
        String symbol,
        int quantity,
        double entryPrice,
        double exitPrice,
        String exitReason
    ) {
        if (lifecycleState == TradeLifecycleState.FLAT && activeSymbol == null) {
            return; // Already settled
        }

        int finalQty = quantity > 0 ? quantity : (currentProtectedQuantity > 0 ? currentProtectedQuantity : 1);
        double finalEntry = entryPrice > 0 ? entryPrice : (confirmedEntryPrice > 0 ? confirmedEntryPrice : exitPrice);
        double finalExit = exitPrice > 0 ? exitPrice : finalEntry;

        ChargesBreakdown charges = ChargesBreakdown.calculate(finalQty, finalEntry, finalExit);
        double netPnL = charges.netPnL();
        double grossPnL = charges.grossPnL();

        if (netPnL < 0.0) {
            positionSizingEngine.recordTradeLoss(gateway.getExecutionMode(), Math.abs(netPnL));
            log.warn("REALIZED NET LOSS SETTLED: {} x {} entered @ ₹{}, exited @ ₹{} [Gross: ₹{}, Net: -₹{}, Charges: ₹{}]. Counted towards loss circuit breaker.",
                finalQty, symbol, finalEntry, finalExit, grossPnL, Math.abs(netPnL), charges.totalCharges());
        } else {
            log.info("REALIZED NET PROFIT SETTLED: {} x {} entered @ ₹{}, exited @ ₹{} [Gross: ₹{}, Net: +₹{}, Charges: ₹{}].",
                finalQty, symbol, finalEntry, finalExit, grossPnL, netPnL, charges.totalCharges());
        }

        activityFeedService.record(
            "LIFECYCLE",
            netPnL >= 0 ? "SUCCESS" : "WARN",
            gateway.getExecutionMode().name(),
            activeTradeId != null ? activeTradeId : "TRADE-" + symbol,
            "Trade Settled: " + (netPnL >= 0 ? "Profit" : "Loss"),
            String.format("%s %dx %s: Entry ₹%.2f, Exit ₹%.2f (Net P&L: %s₹%.2f)",
                exitReason, finalQty, symbol, finalEntry, finalExit, (netPnL >= 0 ? "+" : "-"), Math.abs(netPnL)),
            symbol,
            Map.of(
                "symbol", symbol,
                "quantity", finalQty,
                "entryPrice", finalEntry,
                "exitPrice", finalExit,
                "grossPnL", grossPnL,
                "netPnL", netPnL,
                "totalCharges", charges.totalCharges(),
                "exitReason", exitReason
            )
        );

        if (gateway.getExecutionMode() == ExecutionMode.LIVE && liveTradeRepository != null) {
            try {
                liveTradeRepository.save(new com.algotrade.algotradebackend.persistence.entity.LiveTradeEntity(
                    symbol,
                    finalQty,
                    finalEntry,
                    finalExit,
                    Instant.now().minusSeconds(60),
                    Instant.now(),
                    exitReason,
                    grossPnL,
                    netPnL,
                    charges.totalCharges(),
                    charges.brokerage(),
                    charges.stt(),
                    charges.exchangeCharges(),
                    charges.sebiCharges(),
                    charges.stampDuty(),
                    charges.gst(),
                    activeTradeId
                ));
            } catch (Throwable t) {
                log.error("Failed to persist LiveTradeEntity: {}", t.getMessage());
            }
        }

        setFlatState();
        broadcastOverviewIfAvailable();
    }

    private void setFlatState() {
        lifecycleState = TradeLifecycleState.FLAT;
        activeSymbol = null;
        activeTradeId = null;
        activeSlOrderId = null;
        activeEntryOrderId = null;
        activeExitOrderId = null;
        strategyHighWaterMark = 0.0;
        currentArmedStopPrice = 0.0;
        confirmedEntryPrice = 0.0;
        plannedStopPrice = 0.0;
        plannedQuantity = 0;
        currentProtectedQuantity = 0;
        exitOrderInFlight.set(false);
        lastExitOrderDispatchMs.set(0L);
        entryOrderTimestampMs = 0L;
        logicalStage = 0;
        armedPair = new StopLadder.StopOrderPair(0.0, 0.0);
        initialStructuralStop = 0.0;
        initialStructuralLimit = 0.0;
        unitRisk = 0.0;
        entryFillTimeUtc = null;
        hwmTimestampUtc = null;
        hwm15MinAgo = 0.0;
        lastHwmStepTsUtc = null;
        lastStopModifyTimeMs = 0L;
        slModificationCount = 0;
        currentOrderTag = null;
        peak06Latched = false;
        incubDoneLatched = false;
        stagDoneLatched = false;
        t45DoneLatched = false;
        eodDoneLatched = false;
        activeExitPolicy = "NONE";
        emergencyStep = 0;
        lastEmergencyStepTimeMs = 0L;
        protectedGraceStartTimeMs = 0L;
        emergencyUnfilledStartTimeMs = 0L;
        if (paperActivePositionRepository != null) {
            try {
                paperActivePositionRepository.deleteAll();
            } catch (Throwable ignored) {}
        }
    }

    private void broadcastOverviewIfAvailable() {
        if (broadcastServiceProvider != null && broadcastServiceProvider.getIfAvailable() != null) {
            try {
                broadcastServiceProvider.getIfAvailable().broadcastOverview();
            } catch (Throwable t) {
                log.warn("Failed to broadcast overview update: {}", t.getMessage());
            }
        }
    }

    private void cancelRestingOrders(BrokerGateway gateway) {
        if (activeEntryOrderId != null) {
            try {
                gateway.cancelOrder(activeEntryOrderId);
            } catch (Throwable t) {
                log.warn("Failed to cancel resting entry order {}: {}", activeEntryOrderId, t.getMessage());
            }
            activeEntryOrderId = null;
        }
        if (activeSlOrderId != null) {
            try {
                gateway.cancelOrder(activeSlOrderId);
            } catch (Throwable t) {
                log.warn("Failed to cancel resting SL order {}: {}", activeSlOrderId, t.getMessage());
            }
            activeSlOrderId = null;
        }
    }

    @Override
    public synchronized Map<String, Object> executeManualExit(String expectedSymbol, String reason) {
        BrokerGateway gateway = brokerGatewayManager.getActiveGateway();
        Optional<Position> optPos = gateway.getOpenPosition();

        if (optPos.isEmpty() || optPos.get().quantity() <= 0) {
            cancelRestingOrders(gateway);
            setFlatState();
            broadcastOverviewIfAvailable();
            Map<String, Object> res = new LinkedHashMap<>();
            res.put("success", true);
            res.put("message", "No active position open to liquidate. Engine is flat.");
            res.put("status", "FLAT");
            return res;
        }

        Position pos = optPos.get();
        if (expectedSymbol != null && !expectedSymbol.isBlank() && !pos.symbol().equalsIgnoreCase(expectedSymbol)) {
            log.warn("Manual exit symbol mismatch: requested {}, open position is {}", expectedSymbol, pos.symbol());
        }

        String exitReason = (reason != null && !reason.isBlank()) ? reason : "MANUAL_OPERATOR_SELL";
        log.info("DISPATCHING MANUAL EXIT for {} x {} (Reason: {})", pos.quantity(), pos.symbol(), exitReason);

        // 1. Immediately cancel resting entry orders
        if (activeEntryOrderId != null) {
            try { gateway.cancelOrder(activeEntryOrderId); } catch (Throwable ignored) {}
            activeEntryOrderId = null;
        }

        // 2. Dispatch PROTECTED or EMERGENCY exit ladder
        lifecycleState = TradeLifecycleState.PENDING_EXIT;
        exitOrderInFlight.set(true);
        lastExitOrderDispatchMs.set(System.currentTimeMillis());
        double executedPrice = pos.currentPrice();

        TradeOrder exitOrder;
        double bestBid = gateway.getBestBid(pos.symbol()).orElse(pos.currentPrice());
        double floorPrice = armedPair.limitPrice();
        if (logicalStage >= 2 && bestBid >= floorPrice) {
            log.info("MANUAL LIQUIDATE: Executing PROTECTED_EXIT at floor ₹{} (Bid=₹{})", floorPrice, bestBid);
            exitOrder = executeProtectedExit(gateway, pos, floorPrice);
        } else {
            log.warn("MANUAL LIQUIDATE: Executing EMERGENCY_EXIT ladder (Bid=₹{})", bestBid);
            exitOrder = executeEmergencyExit(gateway, pos, exitReason);
        }
        if (exitOrder != null) {
            activeExitOrderId = exitOrder.orderId();
            if (exitOrder.price() > 0) {
                executedPrice = exitOrder.price();
            }
        }

        // 3. Check if broker confirms fill immediately (Paper simulation or instant broker execution)
        boolean isInstantlyFilled = exitOrder != null && "FILLED".equalsIgnoreCase(exitOrder.status());
        if (isInstantlyFilled || gateway.getExecutionMode() == ExecutionMode.PAPER) {
            handleTradeCompleted(gateway, pos.symbol(), pos.quantity(), pos.entryPrice(), executedPrice, exitReason);
        } else {
            // Live execution: trigger async sync but remain in PENDING_EXIT until confirmed flat
            if (gateway instanceof com.algotrade.algotradebackend.broker.zerodha.ZerodhaLiveGateway liveGw) {
                liveGw.syncPositionsFromBroker();
                var confirmedPos = liveGw.getOpenPosition();
                if (confirmedPos.isEmpty() || confirmedPos.get().quantity() <= 0) {
                    handleTradeCompleted(gateway, pos.symbol(), pos.quantity(), pos.entryPrice(), executedPrice, exitReason);
                }
            }
        }

        // 4. Audit in Activity Feed
        double grossPnL = Math.round(((executedPrice - pos.entryPrice()) * pos.quantity()) * 100.0) / 100.0;
        double grossPnLPct = pos.entryPrice() > 0 ? Math.round(((executedPrice - pos.entryPrice()) / pos.entryPrice() * 100.0) * 100.0) / 100.0 : 0.0;
        activityFeedService.record(
            "ORDERS",
            "SUCCESS",
            gateway.getExecutionMode().name(),
            activeTradeId != null ? activeTradeId : "MANUAL-" + pos.symbol(),
            "Manual Market Exit Executed",
            "Sold " + pos.quantity() + "x " + pos.symbol() + " @ ₹" + executedPrice + " (P&L: " + (grossPnL >= 0 ? "+" : "") + "₹" + grossPnL + " / " + (grossPnLPct >= 0 ? "+" : "") + grossPnLPct + "%). Position " + (lifecycleState == TradeLifecycleState.FLAT ? "closed." : "exit pending fill."),
            pos.symbol(),
            Map.of(
                "symbol", pos.symbol(),
                "quantity", pos.quantity(),
                "entryPrice", pos.entryPrice(),
                "exitPrice", executedPrice,
                "grossPnL", grossPnL,
                "reason", exitReason
            )
        );

        // 5. Broadcast immediate real-time update to all WebSocket connected UI clients
        broadcastOverviewIfAvailable();

        Map<String, Object> result = new LinkedHashMap<>();
        result.put("success", true);
        result.put("message", lifecycleState == TradeLifecycleState.FLAT
            ? "Market exit executed and position closed for " + pos.symbol()
            : "Market exit order dispatched for " + pos.symbol() + ". Awaiting broker fill confirmation.");
        result.put("status", lifecycleState.name());
        result.put("symbol", pos.symbol());
        result.put("quantity", pos.quantity());
        result.put("entryPrice", pos.entryPrice());
        result.put("exitPrice", executedPrice);
        result.put("grossPnL", grossPnL);
        result.put("grossPnLPct", grossPnLPct);
        return result;
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

        if (!positionSizingEngine.canTradeToday()) {
            throw new IllegalStateException("Cannot take manual entry: Daily trade limit or loss circuit breaker reached!");
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

        // Determine Quantity (sized against marketable buy limit / worst-case fill price)
        double marketableBuyLimit = TickMath.ceilToTick(entryPrice * 1.0005, tickSize);
        int quantity;
        if (customQuantity != null && customQuantity > 0) {
            positionSizingEngine.validateManualOrder(symbol, customQuantity, marketableBuyLimit, initialStop, gateway);
            quantity = customQuantity;
        } else {
            quantity = positionSizingEngine.calculateQuantity(symbol, marketableBuyLimit, initialStop, gateway);
            if (quantity < 1) {
                throw new IllegalStateException("Risk sizing calculated 0 shares for " + symbol + " at limit ₹" + marketableBuyLimit + " (check margin / cash / stop distance)");
            }
        }

        String tradeId = "MAN-" + LocalDate.now(ZoneId.of("Asia/Kolkata")) + "-" + symbol;
        activeTradeId = tradeId;
        activeSymbol = symbol;
        plannedStopPrice = initialStop;
        plannedQuantity = quantity;
        lifecycleState = TradeLifecycleState.PENDING_ENTRY;

        log.info("DISPATCHING MANUAL ENTRY ORDER: BUY {} x {} @ Marketable Limit ₹{}", quantity, symbol, marketableBuyLimit);

        TradeOrder buyOrder = gateway.placeOrder(symbol, "BUY", quantity, marketableBuyLimit, 0.0, "LIMIT");
        activeEntryOrderId = buyOrder.orderId();
        positionSizingEngine.recordTradeDispatched(gateway.getExecutionMode());
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
            initialStructuralStop = initialStop;
            StopLadder.StopOrderPair s0 = StopLadder.stage0Candidate(initialStop, tickSize, ladderBufferPct / 100.0);
            initialStructuralLimit = s0.limitPrice();
            armedPair = s0;
            logicalStage = 0;
            unitRisk = pos.entryPrice() - initialStop;
            entryFillTimeUtc = Instant.now();
            hwmTimestampUtc = Instant.now();
            hwm15MinAgo = pos.entryPrice();
            slModificationCount = 0;
            String tag = generateOrderTag("SL");
            currentOrderTag = tag;
            try {
                TradeOrder slOrder = placeOrderWithTagFallback(gateway, symbol, "SELL", pos.quantity(), s0.limitPrice(), s0.triggerPrice(), "SL", tag);
                if (slOrder == null) {
                    throw new IllegalStateException("Gateway returned null stop order for manual trade " + symbol);
                }
                activeSlOrderId = slOrder.orderId();
                currentArmedStopPrice = s0.triggerPrice();
                currentProtectedQuantity = pos.quantity();
                lifecycleState = TradeLifecycleState.ACTIVE_POSITION;
                persistActivePositionState(gateway, pos);
                log.info("PROTECTIVE SL ENGAGED FOR MANUAL TRADE: OrderId={}, Trigger=₹{}, Limit=₹{}, Tag={}", activeSlOrderId, initialStop, s0.limitPrice(), tag);
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

    /**
     * Executes PROTECTED_EXIT per §C9: Limit = max(floor, bestBid), clamped to lower circuit limit.
     * Respects single-working-sell invariant.
     */
    public synchronized TradeOrder executeProtectedExit(BrokerGateway gateway, Position pos, double floorPrice) {
        if (pos == null) return null;
        this.activeExitPolicy = "PROTECTED";
        lifecycleState = TradeLifecycleState.PENDING_EXIT;
        exitOrderInFlight.set(true);
        lastExitOrderDispatchMs.set(System.currentTimeMillis());

        double tickSize = getSymbolTickSize(pos.symbol());
        double bestBid = gateway.getBestBid(pos.symbol()).orElse(pos.currentPrice());
        double lowerCircuit = gateway.getLowerCircuitLimit(pos.symbol()).orElse(pos.currentPrice() * 0.90);
        double limitPrice = StopLadder.protectedExitLimit(floorPrice, bestBid, lowerCircuit);

        // Grace period watchdog: If bid < floor for > 5s, escalate to EMERGENCY
        if (bestBid < floorPrice) {
            if (protectedGraceStartTimeMs == 0L) {
                protectedGraceStartTimeMs = System.currentTimeMillis();
            } else if (System.currentTimeMillis() - protectedGraceStartTimeMs >= 5000L) {
                log.warn("PROTECTED EXIT GRACE EXPIRED: Best bid ₹{} < Floor ₹{} for > 5s. Escalating to EMERGENCY_EXIT.", bestBid, floorPrice);
                return executeEmergencyExit(gateway, pos, "PROTECTED_GRACE_EXPIRED");
            }
        } else {
            protectedGraceStartTimeMs = 0L;
        }

        // Single-working-sell invariant:
        // Case 1: Active exit order already working -> modify in place
        if (activeExitOrderId != null) {
            TradeOrder existing = gateway.getOrderStatus(activeExitOrderId);
            if (existing != null && ("TRIGGER PENDING".equalsIgnoreCase(existing.status()) || "OPEN".equalsIgnoreCase(existing.status()))) {
                int pending = existing.pendingQuantity() > 0 ? existing.pendingQuantity() : existing.quantity();
                log.info("PROTECTED EXIT: In-place modifying exit order {} to Limit ₹{} (Qty={})", activeExitOrderId, limitPrice, pending);
                return gateway.modifyOrder(activeExitOrderId, pending, limitPrice, 0.0);
            }
        }

        // Case 2: Active SL order already OPEN (triggered) -> modify in place
        if (activeSlOrderId != null) {
            TradeOrder slOrder = gateway.getOrderStatus(activeSlOrderId);
            if (slOrder != null && "OPEN".equalsIgnoreCase(slOrder.status())) {
                int pending = slOrder.pendingQuantity() > 0 ? slOrder.pendingQuantity() : slOrder.quantity();
                log.info("PROTECTED EXIT: In-place modifying triggered OPEN stop {} to Limit ₹{} (Qty={})", activeSlOrderId, limitPrice, pending);
                TradeOrder modified = gateway.modifyOrder(activeSlOrderId, pending, limitPrice, 0.0);
                activeExitOrderId = activeSlOrderId;
                return modified;
            } else {
                try {
                    gateway.cancelOrder(activeSlOrderId);
                } catch (Throwable ignored) {}
                activeSlOrderId = null;
            }
        }

        // Case 3: Place new tagged limit sell order
        String tag = generateOrderTag("EX");
        TradeOrder exitOrder = placeOrderWithTagFallback(gateway, pos.symbol(), "SELL", pos.quantity(), limitPrice, 0.0, "LIMIT", tag);
        if (exitOrder != null) {
            activeExitOrderId = exitOrder.orderId();
        }
        log.info("PROTECTED EXIT DISPATCHED: OrderId={}, Symbol={}, Qty={}, Limit=₹{} (Floor=₹{}, Bid=₹{})",
            activeExitOrderId, pos.symbol(), pos.quantity(), limitPrice, floorPrice, bestBid);
        return exitOrder;
    }

    /**
     * Executes EMERGENCY_EXIT per §C9: Escalation ladder starting at Step 1 (t=0) down to Step 4+ (every 3s).
     * Clamped to lower circuit limit. Respects single-working-sell invariant.
     */
    public synchronized TradeOrder executeEmergencyExit(BrokerGateway gateway, Position pos, String reason) {
        if (pos == null) return null;
        this.activeExitPolicy = "EMERGENCY";
        lifecycleState = TradeLifecycleState.PENDING_EXIT;
        exitOrderInFlight.set(true);
        lastExitOrderDispatchMs.set(System.currentTimeMillis());

        long nowMs = System.currentTimeMillis();
        if (emergencyStep == 0) {
            emergencyStep = 1;
            lastEmergencyStepTimeMs = nowMs;
            emergencyUnfilledStartTimeMs = nowMs;
        } else {
            long elapsed = nowMs - lastEmergencyStepTimeMs;
            if (emergencyStep == 1 && elapsed >= 2000L) {
                emergencyStep = 2;
                lastEmergencyStepTimeMs = nowMs;
            } else if (emergencyStep == 2 && elapsed >= 2000L) {
                emergencyStep = 3;
                lastEmergencyStepTimeMs = nowMs;
            } else if (emergencyStep >= 3 && elapsed >= 3000L) {
                emergencyStep++;
                lastEmergencyStepTimeMs = nowMs;
            }
        }

        if (emergencyUnfilledStartTimeMs > 0 && (nowMs - emergencyUnfilledStartTimeMs >= 30000L)) {
            activityFeedService.record(
                "ALERT",
                "CRITICAL",
                gateway.getExecutionMode().name(),
                activeTradeId,
                "Emergency Exit Unfilled > 30s",
                "Emergency exit for " + pos.symbol() + " remains unfilled for > 30s. Escalation continuing...",
                pos.symbol()
            );
        }

        double tickSize = getSymbolTickSize(pos.symbol());
        double bestBid = gateway.getBestBid(pos.symbol()).orElse(pos.currentPrice());
        double lowerCircuit = gateway.getLowerCircuitLimit(pos.symbol()).orElse(pos.currentPrice() * 0.90);
        double emergencyLimit = StopLadder.emergencyExitLimit(bestBid, emergencyStep, tickSize, lowerCircuit);

        log.warn("EMERGENCY EXIT LADDER (Step {}): {} BestBid=₹{}, Limit=₹{}, LowerCircuit=₹{}, Reason={}",
            emergencyStep, pos.symbol(), bestBid, emergencyLimit, lowerCircuit, reason);

        // Single-working-sell invariant:
        // Case 1: Active exit order already working -> modify in place
        if (activeExitOrderId != null) {
            TradeOrder existing = gateway.getOrderStatus(activeExitOrderId);
            if (existing != null && ("TRIGGER PENDING".equalsIgnoreCase(existing.status()) || "OPEN".equalsIgnoreCase(existing.status()))) {
                int pending = existing.pendingQuantity() > 0 ? existing.pendingQuantity() : existing.quantity();
                log.info("EMERGENCY EXIT: In-place modifying exit order {} to Step {} Limit ₹{}", activeExitOrderId, emergencyStep, emergencyLimit);
                return gateway.modifyOrder(activeExitOrderId, pending, emergencyLimit, 0.0);
            }
        }

        // Case 2: Active SL order already OPEN (triggered) -> modify in place
        if (activeSlOrderId != null) {
            TradeOrder slOrder = gateway.getOrderStatus(activeSlOrderId);
            if (slOrder != null && "OPEN".equalsIgnoreCase(slOrder.status())) {
                int pending = slOrder.pendingQuantity() > 0 ? slOrder.pendingQuantity() : slOrder.quantity();
                log.info("EMERGENCY EXIT: In-place modifying triggered OPEN stop {} to Step {} Limit ₹{}", activeSlOrderId, emergencyStep, emergencyLimit);
                TradeOrder modified = gateway.modifyOrder(activeSlOrderId, pending, emergencyLimit, 0.0);
                activeExitOrderId = activeSlOrderId;
                return modified;
            } else {
                try {
                    gateway.cancelOrder(activeSlOrderId);
                } catch (Throwable ignored) {}
                activeSlOrderId = null;
            }
        }

        // Case 3: Place new tagged limit sell order
        String tag = generateOrderTag("EM");
        TradeOrder exitOrder = placeOrderWithTagFallback(gateway, pos.symbol(), "SELL", pos.quantity(), emergencyLimit, 0.0, "LIMIT", tag);
        if (exitOrder != null) {
            activeExitOrderId = exitOrder.orderId();
        }
        return exitOrder;
    }

    private TradeOrder placeOrderWithTagFallback(BrokerGateway gateway, String symbol, String transactionType, int quantity, double price, double triggerPrice, String orderType, String tag) {
        TradeOrder order = gateway.placeOrder(symbol, transactionType, quantity, price, triggerPrice, orderType, tag);
        if (order == null) {
            order = gateway.placeOrder(symbol, transactionType, quantity, price, triggerPrice, orderType);
        }
        return order;
    }

    public String generateOrderTag(String prefix) {
        String pfx = (prefix != null && !prefix.isBlank()) ? prefix : "AT";
        String dateStr = LocalDate.now(ZoneId.of("Asia/Kolkata")).format(java.time.format.DateTimeFormatter.ofPattern("yyMMdd"));
        long seq = System.currentTimeMillis() % 1000000L;
        String tag = pfx + dateStr + String.format(Locale.US, "%06d", seq);
        return tag.length() > 20 ? tag.substring(0, 20) : tag;
    }

    private void persistActivePositionState(BrokerGateway gateway, Position pos) {
        if (paperActivePositionRepository == null || pos == null) return;
        try {
            var opt = paperActivePositionRepository.findFirstByOrderByEntryTimeDesc();
            PaperActivePositionEntity entity = opt.orElseGet(PaperActivePositionEntity::new);
            entity.setSymbol(pos.symbol());
            entity.setQuantity(pos.quantity());
            entity.setEntryPrice(pos.entryPrice());
            entity.setCurrentPrice(pos.currentPrice());
            entity.setInitialStopLoss(initialStructuralStop > 0 ? initialStructuralStop : plannedStopPrice);
            entity.setInitialStopLimit(initialStructuralLimit);
            entity.setActiveTrailingStop(armedPair.triggerPrice());
            entity.setArmedLimit(armedPair.limitPrice());
            entity.setUnitRisk(unitRisk);
            entity.setLogicalStage(logicalStage);
            entity.setSlModCount(slModificationCount);
            entity.setHighWaterMark(strategyHighWaterMark);
            entity.setEntryTime(pos.entryTime() != null ? pos.entryTime() : (entryFillTimeUtc != null ? entryFillTimeUtc : Instant.now()));
            entity.setActiveSlOrderId(activeSlOrderId);
            entity.setExitPolicy(activeExitPolicy);
            entity.setExitOrderId(activeExitOrderId);
            entity.setMode(gateway != null ? gateway.getExecutionMode().name() : ExecutionMode.PAPER.name());
            entity.setHwmTime(hwmTimestampUtc);
            entity.setLastHwmStepTime(lastHwmStepTsUtc);
            
            int mask = 0;
            if (peak06Latched) mask |= 1;
            if (incubDoneLatched) mask |= (1 << 1);
            if (stagDoneLatched) mask |= (1 << 2);
            if (t45DoneLatched) mask |= (1 << 3);
            if (eodDoneLatched) mask |= (1 << 4);
            entity.setLatchesMask(mask);
            entity.setUpdatedAt(Instant.now());
            
            paperActivePositionRepository.save(entity);
        } catch (Throwable t) {
            log.warn("Failed to persist active trade state: {}", t.getMessage());
        }
    }

    public int getLogicalStage() { return logicalStage; }
    public StopLadder.StopOrderPair getArmedPair() { return armedPair; }
    public String getActiveExitPolicy() { return activeExitPolicy; }
    public int getSlModificationCount() { return slModificationCount; }
    public String getActiveSlOrderId() { return activeSlOrderId; }
    public double getCurrentArmedStopPrice() { return currentArmedStopPrice; }
    public boolean isPeak06Latched() { return peak06Latched; }
    public boolean isIncubDoneLatched() { return incubDoneLatched; }
    public boolean isStagDoneLatched() { return stagDoneLatched; }
    public boolean isT45DoneLatched() { return t45DoneLatched; }
    public boolean isEodDoneLatched() { return eodDoneLatched; }
}
