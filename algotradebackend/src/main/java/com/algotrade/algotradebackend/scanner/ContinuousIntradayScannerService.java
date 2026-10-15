package com.algotrade.algotradebackend.scanner;

import com.algotrade.algotradebackend.broker.BrokerGateway;
import com.algotrade.algotradebackend.broker.BrokerGatewayManager;
import com.algotrade.algotradebackend.core.model.ScoredCandidate;
import com.algotrade.algotradebackend.core.service.MarketCalendarService;
import com.algotrade.algotradebackend.risk.PositionSizingEngine;
import com.algotrade.algotradebackend.strategy.StrategyManager;
import com.algotrade.algotradebackend.strategy.TradingStrategy;
import com.algotrade.algotradebackend.strategy.momentum_breakout.MomentumBreakoutStrategy;
import com.algotrade.algotradebackend.websocket.LiveWebSocketBroadcastService;
import com.zerodhatech.models.Quote;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Service;

import java.time.LocalTime;
import java.time.ZoneId;
import java.util.List;
import java.util.Map;

/**
 * Continuous Intraday Breakout & Dynamic Wave Scanner Service.
 * 
 * <p>Executes continuous 1-minute scanning cycles between 09:20 AM and 02:45 PM IST,
 * evaluating all 210 F&O stocks across intraday liquidity waves.</p>
 */
@Service
public class ContinuousIntradayScannerService {

    private static final Logger log = LoggerFactory.getLogger(ContinuousIntradayScannerService.class);
    private static final ZoneId IST_ZONE = ZoneId.of("Asia/Kolkata");

    @Value("${algotrade.scanner.all-day.enabled:true}")
    private boolean enabled;

    private final FnoUniverseService universeService;
    private final PreMarketScannerService preMarketScannerService;
    private final BrokerGatewayManager brokerGatewayManager;
    private final StrategyManager strategyManager;
    private final MarketCalendarService marketCalendarService;
    private final PositionSizingEngine positionSizingEngine;
    private final LiveWebSocketBroadcastService broadcastService;

    public ContinuousIntradayScannerService(
        FnoUniverseService universeService,
        PreMarketScannerService preMarketScannerService,
        BrokerGatewayManager brokerGatewayManager,
        StrategyManager strategyManager,
        MarketCalendarService marketCalendarService,
        PositionSizingEngine positionSizingEngine,
        LiveWebSocketBroadcastService broadcastService
    ) {
        this.universeService = universeService;
        this.preMarketScannerService = preMarketScannerService;
        this.brokerGatewayManager = brokerGatewayManager;
        this.strategyManager = strategyManager;
        this.marketCalendarService = marketCalendarService;
        this.positionSizingEngine = positionSizingEngine;
        this.broadcastService = broadcastService;
    }

    @org.springframework.beans.factory.annotation.Autowired(required = false)
    private org.springframework.beans.factory.ObjectProvider<com.algotrade.algotradebackend.broker.zerodha.KiteMarketDataService> kiteMarketDataProvider;

    @org.springframework.beans.factory.annotation.Autowired(required = false)
    private com.algotrade.algotradebackend.backtest.service.HistoricalMarketDataService historicalMarketDataService;

    private final java.util.concurrent.ConcurrentHashMap<String, com.algotrade.algotradebackend.backtest.service.HistoricalMarketDataService.BaselineReference> baselineCache = new java.util.concurrent.ConcurrentHashMap<>();

    /**
     * Watchdog Timer (Every 5 seconds):
     * Detects silent WebSocket disconnections or stalled tick streams during active market hours.
     * Automatically triggers batched REST quote recovery and socket reconnection.
     */
    @Scheduled(fixedDelay = 5000, initialDelay = 15000)
    public void watchdogTickCheck() {
        if (!enabled || !marketCalendarService.isMarketOpen()) return;
        LocalTime now = LocalTime.now(IST_ZONE);
        if (now.isBefore(LocalTime.of(9, 15)) || now.isAfter(LocalTime.of(15, 30))) return;

        if (kiteMarketDataProvider != null && kiteMarketDataProvider.getIfAvailable() != null) {
            var marketDataService = kiteMarketDataProvider.getIfAvailable();
            long elapsed = System.currentTimeMillis() - marketDataService.getLastTickReceivedMs();
            if (elapsed > 5000 && marketDataService.isConnected()) {
                log.warn("WATCHDOG: Zero ticks received for {} ms across 210 F&O universe. Performing REST recovery...", elapsed);
                executeScan(now);
                marketDataService.reconnectTicker();
            }
        }
    }

    /**
     * Pre-market Baseline Calibration at 09:14:00 AM IST.
     * Pulls full 210 F&O batched quotes before open to establish authoritative PDH and PrevClose baselines.
     */
    @Scheduled(cron = "0 14 9 * * MON-FRI", zone = "Asia/Kolkata")
    public void scheduledPreMarketBaseline() {
        if (!enabled) return;
        log.info("=== RUNNING 09:14:00 AM PRE-MARKET UNIVERSE BASELINE SYNC ===");
        executeScan(LocalTime.of(9, 14));
    }

    /**
     * Executes periodic safety reconciliation and leaderboard telemetry update.
     */
    @Scheduled(fixedDelayString = "${algotrade.scanner.all-day.interval-ms:60000}", initialDelay = 20000)
    public synchronized void runContinuousScanCycle() {
        if (!enabled) return;

        LocalTime now = LocalTime.now(IST_ZONE);

        // Session Window Gate: 09:20 AM to 02:45 PM IST on active market trading days
        if (!marketCalendarService.isMarketOpen()) return;
        if (now.isBefore(LocalTime.of(9, 20)) || now.isAfter(LocalTime.of(14, 45))) return;

        executeScan(now);
    }

    /**
     * On-demand scan execution trigger.
     */
    public synchronized boolean triggerManualScan() {
        LocalTime now = LocalTime.now(IST_ZONE);
        executeScan(now);
        return true;
    }

    /**
     * Core execution: updates real-time quotes, candidate metrics, runs scoring cycle,
     * and broadcasts both LEADERBOARD_UPDATE and ALL_DAY_SCAN_UPDATE to active UI streams.
     */
    public synchronized void executeScan(LocalTime now) {
        TradingStrategy activeStrategy = strategyManager.getActiveStrategy();
        if (!(activeStrategy instanceof MomentumBreakoutStrategy mbStrategy)) {
            return;
        }

        List<String> symbols = universeService.getUniverseSymbols();
        if (symbols == null || symbols.isEmpty()) return;

        Map<String, Quote> quotes = preMarketScannerService.fetchQuotesWithTimeout(symbols);
        if (quotes == null || quotes.isEmpty()) return;

        java.time.LocalDate today = java.time.LocalDate.now(IST_ZONE);

        // Update real-time metrics across the 210 F&O universe
        for (String sym : symbols) {
            Quote q = quotes.get("NSE:" + sym);
            if (q == null) q = quotes.get(sym);
            if (q == null || q.ohlc == null || q.lastPrice <= 0) continue;

            // Ensure baseline reference data is populated for continuous scan candidates
            var existingMetrics = mbStrategy.getMetricsMap().get(sym);
            if (existingMetrics == null || existingMetrics.getPreviousDayHigh() <= 0 || existingMetrics.getPrevClose() <= 0) {
                double prevClose = (q.ohlc.close > 0) ? q.ohlc.close : 0.0;
                double pdh = 0.0;
                double avgVol = 0.0;
                double atrPct = 0.0;

                if (historicalMarketDataService != null) {
                    var ref = baselineCache.computeIfAbsent(sym, s -> historicalMarketDataService.getBaselineReference(s, today));
                    if (ref != null) {
                        if (prevClose <= 0) prevClose = ref.prevClose();
                        pdh = ref.pdh();
                        avgVol = ref.avgDailyVol();
                        atrPct = ref.atrPct();
                    }
                }

                if (pdh <= 0 || prevClose <= 0 || avgVol <= 0) {
                    continue; // Strict Zero Synthetic Data: Skip candidates with unverified baseline
                }

                double openPrice = (q.ohlc.open > 0) ? q.ohlc.open : 0.0;
                mbStrategy.updateReferenceData(sym, prevClose, pdh, avgVol, atrPct, openPrice);
                mbStrategy.setOpeningVolumeBaseline(sym, 0);
            } else if (q.ohlc.open > 0 && (existingMetrics.getOpenPrice() <= 0 || existingMetrics.getLow15() == Double.MAX_VALUE)) {
                existingMetrics.setOfficialOpenPrice(q.ohlc.open);
            }

            long totalBuy = 0, totalSell = 0;
            if (q.depth != null) {
                if (q.depth.buy != null) {
                    for (var d : q.depth.buy) totalBuy += d.getQuantity();
                }
                if (q.depth.sell != null) {
                    for (var d : q.depth.sell) totalSell += d.getQuantity();
                }
            }

            boolean inObsWindow = !now.isBefore(MarketCalendarService.MARKET_OPEN_TIME)
                               && now.isBefore(MarketCalendarService.OBSERVATION_END_TIME);
            mbStrategy.onTick(sym, q.lastPrice, (long) q.volumeTradedToday, totalBuy, totalSell, q.averagePrice, inObsWindow, now);
        }

        // Trigger quantitative evaluation cycle
        mbStrategy.evaluateContinuousCycle();

        // Broadcast real-time scan update telemetry to UI (both standard leaderboard and wave telemetry)
        List<ScoredCandidate> topCandidates = mbStrategy.getLatestTopCandidates();
        broadcastService.broadcastLeaderboardUpdate(topCandidates);
        broadcastService.broadcast("ALL_DAY_SCAN_UPDATE", Map.of(
            "phase", determineMarketPhase(now),
            "timestamp", now.toString(),
            "totalEvaluated", symbols.size(),
            "topCandidates", topCandidates,
            "tradedSymbolsToday", mbStrategy.getTradedSymbolsToday()
        ));
        broadcastService.broadcastOverview();
    }

    public static String determineMarketPhase(LocalTime time) {
        if (time.isBefore(LocalTime.of(9, 0))) return "PRE_MARKET";
        if (time.isBefore(LocalTime.of(9, 15))) return "PRE_OPEN";
        if (time.isBefore(LocalTime.of(9, 20))) return "OPENING_DISCOVERY";
        if (time.isBefore(LocalTime.of(10, 30))) return "WAVE_1_MORNING_MOMENTUM";
        if (time.isBefore(LocalTime.of(12, 30))) return "MIDDAY_CONSOLIDATION";
        if (time.isBefore(LocalTime.of(13, 45))) return "WAVE_2_EUROPEAN_INFLUX";
        if (time.isBefore(LocalTime.of(14, 45))) return "WAVE_3_AFTERNOON_RALLY";
        if (time.isBefore(LocalTime.of(15, 5))) return "SESSION_SHUTDOWN";
        if (time.isBefore(LocalTime.of(15, 30))) return "SQUARE_OFF";
        return "MARKET_CLOSED";
    }

    public boolean isEnabled() { return enabled; }
    public void setEnabled(boolean enabled) { this.enabled = enabled; }
}
