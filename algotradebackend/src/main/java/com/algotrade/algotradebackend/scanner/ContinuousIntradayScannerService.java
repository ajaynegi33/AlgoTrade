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

    /**
     * Executes every 60 seconds during market hours.
     */
    @Scheduled(fixedDelayString = "${algotrade.scanner.all-day.interval-ms:60000}", initialDelay = 15000)
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

        // Update real-time metrics across the 210 F&O universe
        for (String sym : symbols) {
            Quote q = quotes.get("NSE:" + sym);
            if (q == null) q = quotes.get(sym);
            if (q == null || q.ohlc == null || q.lastPrice <= 0) continue;

            double prevClose = q.ohlc.close;
            double pdh = q.ohlc.high > 0 ? q.ohlc.high : prevClose;
            mbStrategy.updateReferenceData(sym, prevClose, pdh, 2_000_000.0, 2.0);

            long totalBuy = 0, totalSell = 0;
            if (q.depth != null) {
                if (q.depth.buy != null) {
                    for (var d : q.depth.buy) totalBuy += d.getQuantity();
                }
                if (q.depth.sell != null) {
                    for (var d : q.depth.sell) totalSell += d.getQuantity();
                }
            }

            mbStrategy.onTick(sym, q.lastPrice, (long) q.volumeTradedToday, totalBuy, totalSell, q.averagePrice, true, now);
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
        if (time.isBefore(LocalTime.of(9, 20))) return "OPENING_DISCOVERY";
        if (time.isBefore(LocalTime.of(10, 30))) return "WAVE_1_MORNING_MOMENTUM";
        if (time.isBefore(LocalTime.of(12, 30))) return "MIDDAY_CONSOLIDATION";
        if (time.isBefore(LocalTime.of(13, 45))) return "WAVE_2_EUROPEAN_INFLUX";
        if (time.isBefore(LocalTime.of(14, 45))) return "WAVE_3_AFTERNOON_RALLY";
        if (time.isBefore(LocalTime.of(15, 5))) return "SESSION_SHUTDOWN";
        return "SQUARE_OFF";
    }

    public boolean isEnabled() { return enabled; }
    public void setEnabled(boolean enabled) { this.enabled = enabled; }
}
