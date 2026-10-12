package com.algotrade.algotradebackend.strategy.momentum_breakout;

import com.algotrade.algotradebackend.broker.BrokerGateway;
import com.algotrade.algotradebackend.broker.BrokerGatewayManager;
import com.algotrade.algotradebackend.core.model.Position;
import com.algotrade.algotradebackend.core.model.ScoredCandidate;
import com.algotrade.algotradebackend.core.model.TradeOrder;
import com.algotrade.algotradebackend.risk.PositionSizingEngine;
import com.algotrade.algotradebackend.strategy.TradingStrategy;
import com.algotrade.algotradebackend.core.service.MarketCalendarService;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;

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
 *   <li><b>09:15–09:30 AM:</b> Ingests ticks and accumulates metrics across liquid watchlist.</li>
 *   <li><b>09:30:00 AM:</b> Scores candidates via 100-pt rubric. Selects top stock with score >= 75.</li>
 *   <li><b>09:30:05 AM:</b> Sizes position using 3 constraints (min Q_risk, Q_margin, Q_exposure). Places entry limit + SL.</li>
 *   <li><b>09:30–11:00 AM:</b> Phase 1 "Let it breathe" (retains initial structural stop at 15-min low).</li>
 *   <li><b>11:00–15:15 PM:</b> Phase 2 Trailing buffer (ratchets stop at 2% below High-Water Mark via modifyOrder).</li>
 *   <li><b>15:15:00 PM:</b> Phase 3 Mandatory square-off.</li>
 * </ol>
 */
@Service
public class MomentumBreakoutStrategy implements TradingStrategy {

    private static final Logger log = LoggerFactory.getLogger(MomentumBreakoutStrategy.class);

    private final MomentumScoringEngine scoringEngine;
    private final PositionSizingEngine positionSizingEngine;
    private final BrokerGatewayManager brokerGatewayManager;
    private final MarketCalendarService marketCalendarService;

    private final ConcurrentHashMap<String, CandidateMetrics> metricsMap = new ConcurrentHashMap<>();
    private final CopyOnWriteArrayList<ScoredCandidate> latestTopCandidates = new CopyOnWriteArrayList<>();

    // Active trade tracking
    private volatile String activeSymbol = null;
    private volatile String activeSlOrderId = null;

    public MomentumBreakoutStrategy(
        MomentumScoringEngine scoringEngine,
        PositionSizingEngine positionSizingEngine,
        BrokerGatewayManager brokerGatewayManager,
        MarketCalendarService marketCalendarService
    ) {
        this.scoringEngine = scoringEngine;
        this.positionSizingEngine = positionSizingEngine;
        this.brokerGatewayManager = brokerGatewayManager;
        this.marketCalendarService = marketCalendarService;
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
        // (i) Stop stock observation and tick processing during off-market hours
        if (!marketCalendarService.isMarketOpen()) {
            return;
        }

        CandidateMetrics metrics = metricsMap.get(symbol);
        if (metrics != null) {
            metrics.updateTick(ltp, volume, totalBuyQty, totalSellQty);
        }

        // Active Trade Trailing Management (Post-11:00 AM)
        if (symbol.equals(activeSymbol)) {
            BrokerGateway gateway = brokerGatewayManager.getActiveGateway();
            Optional<Position> optPos = gateway.getOpenPosition();

            if (optPos.isPresent()) {
                Position pos = optPos.get();
                LocalTime now = LocalTime.now(ZoneId.of("Asia/Kolkata"));

                // Phase 2: Trailing Buffer active only after 11:00 AM IST
                if (now.isAfter(LocalTime.of(11, 0)) && activeSlOrderId != null) {
                    double highWaterMark = Math.max(pos.highWaterMark(), ltp);
                    // Trail 2.0% below the highest price achieved since entry
                    double targetTrailStop = Math.round((highWaterMark * 0.980) * 20.0) / 20.0; // round to ₹0.05 tick

                    // Ratchet stop strictly upward if higher than current stop
                    if (targetTrailStop > pos.activeTrailingStop() && targetTrailStop > pos.initialStopLoss()) {
                        double limitPrice = Math.round((targetTrailStop * 0.998) * 20.0) / 20.0;
                        log.info("RATCHETING TRAIL STOP for {}: HWM=₹{}, New Trigger=₹{}, Limit=₹{}",
                            symbol, highWaterMark, targetTrailStop, limitPrice);
                        gateway.modifyOrder(activeSlOrderId, limitPrice, targetTrailStop);
                    }
                }
            } else {
                // Position closed
                activeSymbol = null;
                activeSlOrderId = null;
            }
        }
    }

    @Override
    public synchronized void evaluate0930() {
        log.info("=== RUNNING 09:30:00 SCORING EVALUATION ===");
        BrokerGateway gateway = brokerGatewayManager.getActiveGateway();

        // Safety Gate: Ensure no position is already open and daily limit is not reached
        if (gateway.getOpenPosition().isPresent()) {
            log.warn("Evaluation skipped: A position is already actively held.");
            return;
        }

        if (!positionSizingEngine.canTradeToday()) {
            log.warn("Evaluation skipped: 1-trade-per-day limit already reached.");
            return;
        }

        // Step 1: Score all candidates
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

        // Step 3: Check qualification threshold (Score >= 75)
        if (ranked.isEmpty() || !ranked.get(0).isQualified()) {
            log.warn("NO TRADE TODAY: Top candidate did not meet the 75-point qualification threshold.");
            return;
        }

        ScoredCandidate winner = ranked.get(0);
        CandidateMetrics winnerMetrics = metricsMap.get(winner.symbol());

        // Step 4: Calculate Structural Stop and Position Size
        double entryPrice = winner.ltp();
        // Initial stop strictly anchored to 15-min low minus 5 paise tick buffer
        double initialStop = Math.round((winnerMetrics.getLow15() - 0.05) * 20.0) / 20.0;

        int quantity = positionSizingEngine.calculateQuantity(winner.symbol(), entryPrice, initialStop, gateway);
        if (quantity < 1) {
            log.warn("Trade aborted for {}: Calculated quantity is 0 shares.", winner.symbol());
            return;
        }

        // Step 5: Place Marketable Limit Buy Order
        double marketableBuyLimit = Math.round((entryPrice * 1.0005) * 20.0) / 20.0; // LTP + 0.05%
        log.info("DISPATCHING ENTRY ORDER: BUY {} x {} @ Marketable Limit ₹{}", quantity, winner.symbol(), marketableBuyLimit);

        TradeOrder buyOrder = gateway.placeOrder(winner.symbol(), "BUY", quantity, marketableBuyLimit, 0.0, "LIMIT");
        activeSymbol = winner.symbol();

        // Step 6: Place Immediate Protective Stop-Loss Order
        double slLimitPrice = Math.round((initialStop * 0.998) * 20.0) / 20.0;
        TradeOrder slOrder = gateway.placeOrder(winner.symbol(), "SELL", quantity, slLimitPrice, initialStop, "SL");
        activeSlOrderId = slOrder.orderId();

        log.info("PROTECTIVE SL ENGAGED: OrderId={}, Trigger=₹{}, Limit=₹{}", activeSlOrderId, initialStop, slLimitPrice);
    }

    @Override
    public synchronized void executeSquareOff1515() {
        log.info("=== 15:15:00 MANDATORY INTRADAY SQUARE-OFF ENGAGED ===");
        BrokerGateway gateway = brokerGatewayManager.getActiveGateway();
        Optional<Position> optPos = gateway.getOpenPosition();

        if (optPos.isPresent()) {
            Position pos = optPos.get();
            log.warn("SQUARING OFF ACTIVE POSITION: {} x {}", pos.quantity(), pos.symbol());

            // Cancel resting stop order first
            if (activeSlOrderId != null) {
                gateway.cancelOrder(activeSlOrderId);
            }

            // Aggressive limit sell: Best Bid - 0.10%
            double aggressiveSellPrice = Math.round((pos.currentPrice() * 0.9990) * 20.0) / 20.0;
            gateway.placeOrder(pos.symbol(), "SELL", pos.quantity(), aggressiveSellPrice, 0.0, "LIMIT");

            activeSymbol = null;
            activeSlOrderId = null;
        } else {
            log.info("Square-off complete: No open position found.");
        }
    }

    @Override
    public List<ScoredCandidate> getLatestTopCandidates() {
        return Collections.unmodifiableList(latestTopCandidates);
    }

    public Map<String, CandidateMetrics> getMetricsMap() {
        return metricsMap;
    }
}
