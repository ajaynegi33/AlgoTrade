package com.algotrade.algotradebackend.websocket;

import com.algotrade.algotradebackend.broker.BrokerGatewayManager;
import com.algotrade.algotradebackend.core.model.ExecutionMode;
import com.algotrade.algotradebackend.core.model.Position;
import com.algotrade.algotradebackend.core.service.EngineActivityFeedService;
import com.algotrade.algotradebackend.core.service.MarketCalendarService;
import com.algotrade.algotradebackend.risk.PositionSizingEngine;
import com.algotrade.algotradebackend.scanner.ContinuousIntradayScannerService;
import com.algotrade.algotradebackend.scanner.PreMarketScannerService;
import com.algotrade.algotradebackend.core.model.ScoredCandidate;
import com.algotrade.algotradebackend.strategy.StrategyManager;
import com.algotrade.algotradebackend.strategy.momentum_breakout.MomentumBreakoutStrategy;
import com.google.gson.Gson;
import com.google.gson.GsonBuilder;
import com.google.gson.JsonPrimitive;
import com.google.gson.JsonSerializer;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.ObjectProvider;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Component;
import org.springframework.web.socket.CloseStatus;
import org.springframework.web.socket.TextMessage;
import org.springframework.web.socket.WebSocketSession;
import org.springframework.web.socket.handler.TextWebSocketHandler;

import java.io.IOException;
import java.time.Instant;
import java.time.LocalTime;
import java.util.*;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Thread-safe native WebSocket handler for the AlgoTrade live stream.
 * 
 * <p>Handles:
 * <ul>
 *   <li><b>Session Lifecycle:</b> Clean connection registration and graceful cleanup on close.</li>
 *   <li><b>Initial Snapshot:</b> Immediately streams full trading state on connect (0 ms UI lag).</li>
 *   <li><b>Heartbeat Ping/Pong:</b> Responds to client PINGs to keep NAT/proxy active.</li>
 *   <li><b>Thread-Safe Broadcasts:</b> Synchronizes socket writes to prevent frame collisions.</li>
 * </ul>
 * </p>
 */
@Component
public class LiveEngineWebSocketHandler extends TextWebSocketHandler {

    private static final Logger log = LoggerFactory.getLogger(LiveEngineWebSocketHandler.class);

    private final BrokerGatewayManager brokerGatewayManager;
    private final StrategyManager strategyManager;
    private final PositionSizingEngine positionSizingEngine;
    private final MarketCalendarService marketCalendarService;
    private final EngineActivityFeedService activityFeedService;
    private final ObjectProvider<PreMarketScannerService> scannerProvider;
    private final Gson gson;

    private final Map<String, WebSocketSession> activeSessions = new ConcurrentHashMap<>();
    private final Map<String, WebSocketSession> pendingAuthSessions = new ConcurrentHashMap<>();
    private final java.util.concurrent.ScheduledExecutorService authScheduler = java.util.concurrent.Executors.newSingleThreadScheduledExecutor(r -> {
        Thread t = new Thread(r, "ws-auth-timeout");
        t.setDaemon(true);
        return t;
    });

    private final com.algotrade.algotradebackend.security.JwtTokenService jwtTokenService;

    @Autowired
    public LiveEngineWebSocketHandler(
        BrokerGatewayManager brokerGatewayManager,
        @org.springframework.context.annotation.Lazy StrategyManager strategyManager,
        PositionSizingEngine positionSizingEngine,
        MarketCalendarService marketCalendarService,
        EngineActivityFeedService activityFeedService,
        ObjectProvider<PreMarketScannerService> scannerProvider,
        @Autowired(required = false) com.algotrade.algotradebackend.security.JwtTokenService jwtTokenService
    ) {
        this.brokerGatewayManager = brokerGatewayManager;
        this.strategyManager = strategyManager;
        this.positionSizingEngine = positionSizingEngine;
        this.marketCalendarService = marketCalendarService;
        this.activityFeedService = activityFeedService;
        this.scannerProvider = scannerProvider;
        this.jwtTokenService = jwtTokenService;
        this.gson = new GsonBuilder()
            .registerTypeAdapter(Instant.class, (JsonSerializer<Instant>) (src, type, ctx) -> new JsonPrimitive(src.toString()))
            .registerTypeAdapter(java.time.LocalDate.class, (JsonSerializer<java.time.LocalDate>) (src, type, ctx) -> new JsonPrimitive(src.toString()))
            .registerTypeAdapter(java.time.LocalDateTime.class, (JsonSerializer<java.time.LocalDateTime>) (src, type, ctx) -> new JsonPrimitive(src.toString()))
            .create();
    }

    public LiveEngineWebSocketHandler(
        BrokerGatewayManager brokerGatewayManager,
        StrategyManager strategyManager,
        PositionSizingEngine positionSizingEngine,
        MarketCalendarService marketCalendarService,
        EngineActivityFeedService activityFeedService,
        ObjectProvider<PreMarketScannerService> scannerProvider
    ) {
        this(brokerGatewayManager, strategyManager, positionSizingEngine, marketCalendarService, activityFeedService, scannerProvider, null);
    }

    @Override
    public void afterConnectionEstablished(WebSocketSession session) {
        if (jwtTokenService != null) {
            String token = extractToken(session);
            if (token != null && jwtTokenService.isValidToken(token)) {
                registerAuthenticatedSession(session);
                return;
            }

            // Put into pending auth with a 3-second grace window to receive an in-band AUTH frame
            pendingAuthSessions.put(session.getId(), session);
            authScheduler.schedule(() -> {
                if (pendingAuthSessions.remove(session.getId()) != null) {
                    try {
                        log.warn("WebSocket session {} timed out waiting for in-band AUTH frame. Closing.", session.getId());
                        session.close(CloseStatus.POLICY_VIOLATION.withReason("Authentication timeout"));
                    } catch (IOException ignored) {}
                }
            }, 3, java.util.concurrent.TimeUnit.SECONDS);
            return;
        }

        registerAuthenticatedSession(session);
    }

    private void registerAuthenticatedSession(WebSocketSession session) {
        activeSessions.put(session.getId(), session);
        log.info("New authenticated WebSocket connection established: ID={}. Total active clients: {}",
            session.getId(), activeSessions.size());

        // Send immediate comprehensive snapshot so UI renders instantly with zero polling
        sendInitialSnapshot(session);
    }

    private String extractToken(WebSocketSession session) {
        java.net.URI uri = session.getUri();
        if (uri != null && uri.getQuery() != null) {
            String query = uri.getQuery();
            for (String param : query.split("&")) {
                String[] pair = param.split("=", 2);
                if (pair.length == 2 && "token".equalsIgnoreCase(pair[0])) {
                    return pair[1];
                }
            }
        }
        if (session.getHandshakeHeaders() != null) {
            List<String> authHeaders = session.getHandshakeHeaders().get("Authorization");
            if (authHeaders != null && !authHeaders.isEmpty()) {
                String authHeader = authHeaders.get(0);
                if (authHeader.startsWith("Bearer ")) {
                    return authHeader.substring(7);
                }
            }
        }
        return null;
    }

    @Autowired(required = false)
    private ObjectProvider<com.algotrade.algotradebackend.broker.zerodha.KiteMarketDataService> kiteMarketDataProvider;

    @Autowired(required = false)
    private ObjectProvider<ContinuousIntradayScannerService> continuousScannerProvider;

    @Override
    protected void handleTextMessage(WebSocketSession session, TextMessage message) {
        String payload = message.getPayload().trim();
        if (payload.contains("PING") || payload.equalsIgnoreCase("ping")) {
            sendSafeMessage(session, "{\"type\":\"PONG\",\"timestamp\":\"" + Instant.now() + "\"}");
            return;
        }

        try {
            com.google.gson.JsonObject json = com.google.gson.JsonParser.parseString(payload).getAsJsonObject();
            if (json.has("type") && "AUTH".equalsIgnoreCase(json.get("type").getAsString())) {
                String token = json.has("token") ? json.get("token").getAsString() : null;
                if (jwtTokenService != null && (token == null || !jwtTokenService.isValidToken(token))) {
                    pendingAuthSessions.remove(session.getId());
                    session.close(CloseStatus.POLICY_VIOLATION.withReason("Unauthorized: Invalid JWT"));
                    return;
                }
                pendingAuthSessions.remove(session.getId());
                registerAuthenticatedSession(session);
                sendSafeMessage(session, "{\"type\":\"AUTH_SUCCESS\",\"timestamp\":\"" + Instant.now() + "\"}");
                return;
            }

            // If session is still pending auth, ignore other commands
            if (pendingAuthSessions.containsKey(session.getId())) {
                log.warn("Ignored command from unauthenticated session {}", session.getId());
                return;
            }

            if (json.has("action") && "SUBSCRIBE_SYMBOL".equalsIgnoreCase(json.get("action").getAsString())) {
                if (json.has("symbol")) {
                    String sym = json.get("symbol").getAsString();
                    if (kiteMarketDataProvider != null && kiteMarketDataProvider.getIfAvailable() != null) {
                        kiteMarketDataProvider.getIfAvailable().subscribeSymbol(sym);
                    }
                }
            }
        } catch (Throwable ignored) {}
    }

    @Override
    public void afterConnectionClosed(WebSocketSession session, CloseStatus status) {
        activeSessions.remove(session.getId());
        pendingAuthSessions.remove(session.getId());
        log.info("WebSocket connection closed: ID={}, Code={}, Reason={}. Remaining clients: {}",
            session.getId(), status.getCode(), status.getReason(), activeSessions.size());
    }

    @Override
    public void handleTransportError(WebSocketSession session, Throwable exception) {
        log.warn("WebSocket transport error on session {}: {}", session.getId(), exception.getMessage());
        activeSessions.remove(session.getId());
        try {
            if (session.isOpen()) {
                session.close(CloseStatus.SERVER_ERROR);
            }
        } catch (Throwable ignored) {}
    }

    /**
     * Broadcasts a JSON message to all currently connected and open WebSocket clients.
     */
    public void broadcast(String jsonMessage) {
        if (activeSessions.isEmpty()) return;

        TextMessage textMessage = new TextMessage(jsonMessage);
        for (WebSocketSession session : activeSessions.values()) {
            synchronized (session) {
                try {
                    if (session.isOpen()) {
                        session.sendMessage(textMessage);
                    } else {
                        activeSessions.remove(session.getId());
                    }
                } catch (IOException e) {
                    activeSessions.remove(session.getId());
                    log.debug("Client {} disconnected during broadcast: {}", session.getId(), e.getMessage());
                }
            }
        }
    }

    /**
     * Sends the complete state snapshot immediately when a new browser connects.
     */
    public void sendInitialSnapshot(WebSocketSession session) {
        try {
            Map<String, Object> snapshotData = new LinkedHashMap<>();
            Map<String, Object> overviewData = buildOverviewData();
            snapshotData.put("overview", overviewData);
            if (overviewData.get("activePosition") instanceof Map<?, ?> posMap) {
                Object symObj = posMap.get("symbol");
                if (symObj instanceof String sym && kiteMarketDataProvider != null && kiteMarketDataProvider.getIfAvailable() != null) {
                    kiteMarketDataProvider.getIfAvailable().subscribeSymbol(sym);
                }
            }
            List<ScoredCandidate> initialCandidates = strategyManager.getLatestTopCandidates();
            if (initialCandidates.isEmpty() && continuousScannerProvider != null && continuousScannerProvider.getIfAvailable() != null) {
                try {
                    continuousScannerProvider.getIfAvailable().triggerManualScan();
                    initialCandidates = strategyManager.getLatestTopCandidates();
                } catch (Throwable t) {
                    log.warn("Failed to trigger initial baseline scan on WS connect: {}", t.getMessage());
                }
            }
            LocalTime now = LocalTime.now(MarketCalendarService.IST_ZONE);
            String currentPhase = marketCalendarService.isMarketOpen()
                ? ContinuousIntradayScannerService.determineMarketPhase(now)
                : "MARKET_CLOSED";
            snapshotData.put("marketPhase", currentPhase);
            snapshotData.put("phase", currentPhase);
            snapshotData.put("leaderboard", initialCandidates);
            snapshotData.put("recentDecisions", activityFeedService.getRecentEvents(50));
            if (scannerProvider != null && scannerProvider.getIfAvailable() != null) {
                snapshotData.put("shortlistedCandidates", scannerProvider.getIfAvailable().getShortlistedCandidates());
            }
            if (brokerGatewayManager != null && brokerGatewayManager.getZerodhaLiveGateway() != null) {
                snapshotData.put("zerodhaSession", brokerGatewayManager.getZerodhaLiveGateway().getSessionDetails());
            }

            Map<String, Object> frame = new LinkedHashMap<>();
            frame.put("type", "INITIAL_SNAPSHOT");
            frame.put("timestamp", Instant.now().toString());
            frame.put("data", snapshotData);

            String json = gson.toJson(frame);
            sendSafeMessage(session, json);
        } catch (Throwable t) {
            log.error("Failed to compile and send initial snapshot to {}: {}", session.getId(), t.getMessage(), t);
        }
    }

    /**
     * Builds the overview data structure identical to GET /api/dashboard/overview.
     */
    public Map<String, Object> buildOverviewData() {
        Map<String, Object> response = new LinkedHashMap<>();

        ExecutionMode mode = brokerGatewayManager.getActiveMode();
        Optional<Position> optPos = brokerGatewayManager.getActiveGateway().getOpenPosition();

        LocalTime now = LocalTime.now(MarketCalendarService.IST_ZONE);
        String currentPhase = marketCalendarService.isMarketOpen()
            ? ContinuousIntradayScannerService.determineMarketPhase(now)
            : "MARKET_CLOSED";
        response.put("marketPhase", currentPhase);
        response.put("phase", currentPhase);

        response.put("mode", mode.name());
        response.put("strategyId", strategyManager.getActiveStrategy() != null ? strategyManager.getActiveStrategy().getStrategyId() : "NONE");
        response.put("strategyName", strategyManager.getActiveStrategy() != null ? strategyManager.getActiveStrategy().getStrategyName() : "None");
        response.put("capital", positionSizingEngine.getCapital());
        response.put("baseCapital", positionSizingEngine.getBaseCapital());
        response.put("cumulativeNetPnL", positionSizingEngine.getCumulativeNetPnL());
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
            if (strategyManager.getActiveStrategy() instanceof MomentumBreakoutStrategy mbs) {
                posData.put("logicalStage", mbs.getLogicalStage());
                posData.put("armedLimit", mbs.getArmedPair() != null ? mbs.getArmedPair().limitPrice() : null);
                posData.put("armedTrigger", mbs.getArmedPair() != null ? mbs.getArmedPair().triggerPrice() : null);
                posData.put("activeExitPolicy", mbs.getActiveExitPolicy());
                posData.put("slModificationCount", mbs.getSlModificationCount());
            }
            response.put("activePosition", posData);
        } else if (!marketCalendarService.isMarketOpen()) {
            response.put("status", "MARKET_CLOSED");
            response.put("activePosition", null);
        } else {
            response.put("status", positionSizingEngine.canTradeToday() ? "OBSERVING" : "DAY_COMPLETED");
            response.put("activePosition", null);
        }

        return response;
    }

    private void sendSafeMessage(WebSocketSession session, String text) {
        if (!session.isOpen()) return;
        synchronized (session) {
            try {
                session.sendMessage(new TextMessage(text));
            } catch (Throwable t) {
                log.warn("Error sending message to {}: {}", session.getId(), t.getMessage());
            }
        }
    }

    public int getActiveSessionCount() {
        return activeSessions.size();
    }
}
