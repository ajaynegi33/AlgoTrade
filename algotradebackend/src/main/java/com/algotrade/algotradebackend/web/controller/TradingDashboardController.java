package com.algotrade.algotradebackend.web.controller;

import com.algotrade.algotradebackend.broker.BrokerGatewayManager;
import com.algotrade.algotradebackend.core.model.EngineActivityEvent;
import com.algotrade.algotradebackend.core.model.ExecutionMode;
import com.algotrade.algotradebackend.core.model.Position;
import com.algotrade.algotradebackend.core.model.ScoredCandidate;
import com.algotrade.algotradebackend.core.service.EngineActivityFeedService;
import com.algotrade.algotradebackend.core.service.MarketCalendarService;
import com.algotrade.algotradebackend.persistence.entity.PaperTradeEntity;
import com.algotrade.algotradebackend.persistence.repository.PaperTradeRepository;
import com.algotrade.algotradebackend.risk.PositionSizingEngine;
import com.algotrade.algotradebackend.strategy.StrategyManager;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

import java.time.Instant;
import java.time.LocalDate;
import java.time.ZoneId;
import java.util.*;

/**
 * REST API Controller serving the Next.js Frontend Dashboard.
 * 
 * <p>Provides endpoints for:
 * <ul>
 *   <li>System Overview (Active trade, live unrealized PnL, capital status)</li>
 *   <li>Top 10 Performers & 100-Point Scoring Leaderboard</li>
 *   <li>Deep Financial Breakdown (Gross vs Net PnL, all Indian statutory taxes & fees)</li>
 *   <li>Paper vs Live Execution Mode Switching</li>
 *   <li>Emergency Kill-Switch Square-Off</li>
 * </ul>
 * </p>
 */
@RestController
@RequestMapping("/api/dashboard")
@CrossOrigin(origins = "*")
public class TradingDashboardController {

    private final BrokerGatewayManager brokerGatewayManager;
    private final StrategyManager strategyManager;
    private final PositionSizingEngine positionSizingEngine;
    private final PaperTradeRepository paperTradeRepository;
    private final MarketCalendarService marketCalendarService;
    private final EngineActivityFeedService activityFeedService;

    public TradingDashboardController(
        BrokerGatewayManager brokerGatewayManager,
        StrategyManager strategyManager,
        PositionSizingEngine positionSizingEngine,
        PaperTradeRepository paperTradeRepository,
        MarketCalendarService marketCalendarService,
        EngineActivityFeedService activityFeedService
    ) {
        this.brokerGatewayManager = brokerGatewayManager;
        this.strategyManager = strategyManager;
        this.positionSizingEngine = positionSizingEngine;
        this.paperTradeRepository = paperTradeRepository;
        this.marketCalendarService = marketCalendarService;
        this.activityFeedService = activityFeedService;
    }

    /**
     * Overview metrics for the top header and active position card.
     */
    @GetMapping("/overview")
    public ResponseEntity<Map<String, Object>> getOverview() {
        Map<String, Object> response = new LinkedHashMap<>();

        ExecutionMode mode = brokerGatewayManager.getActiveMode();
        Optional<Position> optPos = brokerGatewayManager.getActiveGateway().getOpenPosition();

        response.put("mode", mode.name());
        response.put("strategyId", strategyManager.getActiveStrategy() != null ? strategyManager.getActiveStrategy().getStrategyId() : "NONE");
        response.put("strategyName", strategyManager.getActiveStrategy() != null ? strategyManager.getActiveStrategy().getStrategyName() : "None");
        response.put("capital", positionSizingEngine.getCapital());
        response.put("riskBudget", positionSizingEngine.getRiskBudget());
        response.put("canTradeToday", positionSizingEngine.canTradeToday());
        response.put("isMarketOpen", marketCalendarService.isMarketOpen());
        response.put("nextMarketOpen", marketCalendarService.getNextMarketOpenFormatted());

        if (optPos.isPresent()) {
            Position pos = optPos.get();
            response.put("status", "IN_TRADE");
            Map<String, Object> posData = new LinkedHashMap<>();
            posData.put("symbol", pos.symbol());
            posData.put("quantity", pos.quantity());
            posData.put("entryPrice", pos.entryPrice());
            posData.put("currentPrice", pos.currentPrice());
            posData.put("initialStopLoss", pos.initialStopLoss());
            posData.put("activeTrailingStop", pos.activeTrailingStop());
            posData.put("highWaterMark", pos.highWaterMark());
            posData.put("unrealizedPnL", pos.getUnrealizedPnL());
            posData.put("unrealizedPnLPct", pos.getUnrealizedPnLPct());
            posData.put("entryTime", pos.entryTime().toString());
            response.put("activePosition", posData);
        } else if (!marketCalendarService.isMarketOpen()) {
            // (i) During off-market hours, stop observation
            response.put("status", "MARKET_CLOSED");
            response.put("activePosition", null);
        } else {
            response.put("status", positionSizingEngine.canTradeToday() ? "OBSERVING" : "DAY_COMPLETED");
            response.put("activePosition", null);
        }

        return ResponseEntity.ok(response);
    }

    /**
     * Top 10 Performers & Scoring Leaderboard tab.
     */
    @GetMapping("/leaderboard")
    public ResponseEntity<List<ScoredCandidate>> getLeaderboard() {
        return ResponseEntity.ok(strategyManager.getLatestTopCandidates());
    }

    /**
     * Deep Financial Analytics & Tax Breakdown tab.
     * Computes today's net take-home and all historical statutory taxes paid.
     */
    @GetMapping("/analytics")
    public ResponseEntity<Map<String, Object>> getAnalytics() {
        Map<String, Object> response = new LinkedHashMap<>();

        List<PaperTradeEntity> allTrades = paperTradeRepository.findAllByOrderByExitTimeDesc();

        // Calculate today's session boundaries in IST
        ZoneId istZone = ZoneId.of("Asia/Kolkata");
        Instant startOfDay = LocalDate.now(istZone).atStartOfDay(istZone).toInstant();
        Instant endOfDay = startOfDay.plusSeconds(86400);

        List<PaperTradeEntity> todayTrades = paperTradeRepository.findByEntryTimeBetween(startOfDay, endOfDay);

        // Today's summary
        double todayGrossPnL = todayTrades.stream().mapToDouble(PaperTradeEntity::getGrossPnL).sum();
        double todayNetPnL = todayTrades.stream().mapToDouble(PaperTradeEntity::getNetPnL).sum();
        double todayTotalTaxes = todayTrades.stream().mapToDouble(PaperTradeEntity::getTotalCharges).sum();

        // Cumulative till-date summary
        double cumulativeNetPnL = allTrades.stream().mapToDouble(PaperTradeEntity::getNetPnL).sum();
        double totalGrossWins = allTrades.stream().filter(t -> t.getGrossPnL() > 0).mapToDouble(PaperTradeEntity::getGrossPnL).sum();
        double totalGrossLosses = Math.abs(allTrades.stream().filter(t -> t.getGrossPnL() < 0).mapToDouble(PaperTradeEntity::getGrossPnL).sum());
        double cumulativeTaxesPaid = allTrades.stream().mapToDouble(PaperTradeEntity::getTotalCharges).sum();

        long winCount = allTrades.stream().filter(t -> t.getNetPnL() > 0).count();
        long totalTradesCount = allTrades.size();
        double winRatePct = totalTradesCount > 0 ? (double) winCount / totalTradesCount * 100.0 : 0.0;
        double profitFactor = totalGrossLosses > 0 ? totalGrossWins / totalGrossLosses : totalGrossWins > 0 ? 99.9 : 0.0;

        response.put("todayGrossPnL", Math.round(todayGrossPnL * 100.0) / 100.0);
        response.put("todayNetPnL", Math.round(todayNetPnL * 100.0) / 100.0);
        response.put("todayTotalTaxes", Math.round(todayTotalTaxes * 100.0) / 100.0);
        response.put("todayTradesCount", todayTrades.size());
        response.put("todayTrades", todayTrades);

        response.put("cumulativeNetPnL", Math.round(cumulativeNetPnL * 100.0) / 100.0);
        response.put("totalGrossWins", Math.round(totalGrossWins * 100.0) / 100.0);
        response.put("totalGrossLosses", Math.round(totalGrossLosses * 100.0) / 100.0);
        response.put("cumulativeTaxesPaid", Math.round(cumulativeTaxesPaid * 100.0) / 100.0);
        response.put("winRatePct", Math.round(winRatePct * 10.0) / 10.0);
        response.put("profitFactor", Math.round(profitFactor * 100.0) / 100.0);
        response.put("allTradesHistory", allTrades);

        return ResponseEntity.ok(response);
    }

    /**
     * Real-time Activity Feed / Decision Audit stream.
     * Serves recent algorithmic decisions from lock-free in-memory ring buffer.
     */
    @GetMapping("/activity-feed")
    public ResponseEntity<List<EngineActivityEvent>> getActivityFeed(
            @RequestParam(required = false) Long sinceId,
            @RequestParam(defaultValue = "50") int limit
    ) {
        return ResponseEntity.ok(activityFeedService.getRecentEvents(sinceId, limit));
    }

    /**
     * Toggle Execution Mode (PAPER <-> LIVE).
     */
    @PostMapping("/mode")
    public ResponseEntity<Map<String, Object>> switchMode(@RequestParam String mode) {
        try {
            ExecutionMode targetMode = ExecutionMode.valueOf(mode.toUpperCase());
            brokerGatewayManager.setExecutionMode(targetMode);
            activityFeedService.record(
                "SYSTEM",
                "INFO",
                "Execution Venue Switched",
                "Trading venue switched to " + targetMode.name(),
                null,
                Map.of("targetMode", targetMode.name())
            );
            return ResponseEntity.ok(Map.of("success", true, "mode", targetMode.name()));
        } catch (Exception e) {
            return ResponseEntity.badRequest().body(Map.of("success", false, "error", e.getMessage()));
        }
    }

    /**
     * Emergency Kill-Switch: Forces an immediate market exit of any open position.
     */
    @PostMapping("/emergency-exit")
    public ResponseEntity<Map<String, Object>> emergencyExit() {
        activityFeedService.record(
            "RISK",
            "WARN",
            "Emergency Kill-Switch Triggered",
            "Manual emergency square-off initiated by operator from UI.",
            null,
            Map.of("action", "SQUARE_OFF_ALL", "source", "USER_OPERATOR")
        );
        strategyManager.executeSquareOff1515();
        return ResponseEntity.ok(Map.of("success", true, "message", "Emergency square-off executed"));
    }

    /**
     * Returns the official Zerodha login URL to initiate morning session authentication.
     */
    @GetMapping("/zerodha/login-url")
    public ResponseEntity<Map<String, Object>> getZerodhaLoginUrl() {
        try {
            String url = brokerGatewayManager.getZerodhaLiveGateway().getLoginUrl();
            return ResponseEntity.ok(Map.of("loginUrl", url, "active", brokerGatewayManager.getZerodhaLiveGateway().isSessionActive()));
        } catch (Exception e) {
            return ResponseEntity.badRequest().body(Map.of("error", e.getMessage()));
        }
    }

    /**
     * Completes Zerodha authentication by exchanging request_token for an access_token.
     */
    @PostMapping("/zerodha/session")
    public ResponseEntity<Map<String, Object>> authenticateZerodhaSession(@RequestParam String requestToken) {
        try {
            var userData = brokerGatewayManager.getZerodhaLiveGateway().authenticateSession(requestToken);
            activityFeedService.record(
                "SYSTEM",
                "INFO",
                "Zerodha OAuth Connected",
                "Live Kite Connect session authenticated and verified.",
                null,
                Map.of("gateway", "ZerodhaLiveGateway")
            );
            Map<String, Object> resp = new LinkedHashMap<>();
            resp.put("success", true);
            resp.putAll(userData);
            return ResponseEntity.ok(resp);
        } catch (Throwable e) {
            return ResponseEntity.badRequest().body(Map.of("success", false, "error", e.getMessage() != null ? e.getMessage() : e.toString()));
        }
    }

    /**
     * Headless Automated 2FA Login using TOTP.
     * Generates RFC 6238 TOTP, executes Zerodha web handshake, and sets active session.
     */
    @PostMapping("/zerodha/auto-login")
    public ResponseEntity<Map<String, Object>> autoLoginZerodha(
            @RequestParam(required = false) String password,
            @RequestParam(required = false) String totpSecret) {
        try {
            var userData = brokerGatewayManager.getZerodhaLiveGateway().triggerAutoLogin(password, totpSecret);
            activityFeedService.record(
                "SYSTEM",
                "INFO",
                "Automated 2FA Authenticated",
                "Kite Connect automated TOTP authentication successful.",
                null,
                Map.of("gateway", "ZerodhaLiveGateway", "method", "RFC6238_TOTP")
            );
            Map<String, Object> resp = new LinkedHashMap<>();
            resp.put("success", true);
            resp.put("message", "Automated TOTP authentication successful!");
            resp.putAll(userData);
            return ResponseEntity.ok(resp);
        } catch (Throwable e) {
            String msg = e.getMessage() != null ? e.getMessage() : e.toString();
            return ResponseEntity.badRequest().body(Map.of("success", false, "error", msg));
        }
    }

    /**
     * Checks whether the Zerodha live session is currently authenticated and returns account details.
     */
    @GetMapping("/zerodha/status")
    public ResponseEntity<Map<String, Object>> getZerodhaStatus() {
        return ResponseEntity.ok(brokerGatewayManager.getZerodhaLiveGateway().getSessionDetails());
    }

    /**
     * Automatic OAuth callback endpoint for Zerodha Kite Connect login flow.
     * Zerodha redirects user's browser here after login with ?request_token=...
     */
    @GetMapping("/zerodha/callback")
    public ResponseEntity<Void> handleZerodhaCallback(
            @RequestParam(name = "request_token", required = false) String requestToken,
            @RequestParam(name = "status", required = false) String status,
            @RequestParam(name = "action", required = false) String action) {
        if (requestToken != null && !requestToken.isBlank()) {
            try {
                brokerGatewayManager.getZerodhaLiveGateway().authenticateSession(requestToken);
                return ResponseEntity.status(302).header("Location", "http://localhost:3000/?session=activated").build();
            } catch (Throwable e) {
                String errMsg = e.getMessage() != null ? e.getMessage() : "Authentication failed";
                String encoded = java.net.URLEncoder.encode(errMsg, java.nio.charset.StandardCharsets.UTF_8);
                return ResponseEntity.status(302).header("Location", "http://localhost:3000/?session=error&msg=" + encoded).build();
            }
        }
        return ResponseEntity.status(302).header("Location", "http://localhost:3000/?session=error&msg=No+request_token").build();
    }

    /**
     * Queries comprehensive live quote and 5-level market depth for any NSE stock.
     * Directly calls ZerodhaLiveGateway to enforce 100% real Kite market data.
     */
    @GetMapping("/stock-quote")
    public ResponseEntity<Map<String, Object>> getStockQuote(@RequestParam(defaultValue = "RELIANCE") String symbol) {
        return ResponseEntity.ok(brokerGatewayManager.getZerodhaLiveGateway().getStockQuote(symbol));
    }

    /**
     * Time-series intraday price trajectory for curvy Recharts chart.
     */
    @GetMapping("/stock-chart")
    public ResponseEntity<List<Map<String, Object>>> getStockChart(@RequestParam(defaultValue = "RELIANCE") String symbol) {
        return ResponseEntity.ok(brokerGatewayManager.getZerodhaLiveGateway().getStockChart(symbol));
    }

    /**
     * Returns curated list of high-liquidity NSE stocks for quick selection chips.
     */
    @GetMapping("/popular-stocks")
    public ResponseEntity<List<String>> getPopularStocks() {
        return ResponseEntity.ok(List.of(
            "RELIANCE", "TCS", "INFY", "HDFCBANK", "ICICIBANK",
            "SBIN", "BHARTIARTL", "LT", "AXISBANK", "NTPC",
            "WIPRO", "MARUTI", "SUNPHARMA", "BAJFINANCE"
        ));
    }
}
