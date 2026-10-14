package com.algotrade.algotradebackend.websocket;

import com.algotrade.algotradebackend.broker.BrokerGatewayManager;
import com.algotrade.algotradebackend.core.model.ExecutionMode;
import com.algotrade.algotradebackend.core.model.Position;
import com.algotrade.algotradebackend.core.service.EngineActivityFeedService;
import com.algotrade.algotradebackend.core.service.MarketCalendarService;
import com.algotrade.algotradebackend.risk.PositionSizingEngine;
import com.algotrade.algotradebackend.scanner.PreMarketScannerService;
import com.algotrade.algotradebackend.strategy.StrategyManager;
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

    @Autowired
    public LiveEngineWebSocketHandler(
        BrokerGatewayManager brokerGatewayManager,
        @org.springframework.context.annotation.Lazy StrategyManager strategyManager,
        PositionSizingEngine positionSizingEngine,
        MarketCalendarService marketCalendarService,
        EngineActivityFeedService activityFeedService,
        ObjectProvider<PreMarketScannerService> scannerProvider
    ) {
        this.brokerGatewayManager = brokerGatewayManager;
        this.strategyManager = strategyManager;
        this.positionSizingEngine = positionSizingEngine;
        this.marketCalendarService = marketCalendarService;
        this.activityFeedService = activityFeedService;
        this.scannerProvider = scannerProvider;
        this.gson = new GsonBuilder()
            .registerTypeAdapter(Instant.class, (JsonSerializer<Instant>) (src, type, ctx) -> new JsonPrimitive(src.toString()))
            .registerTypeAdapter(java.time.LocalDate.class, (JsonSerializer<java.time.LocalDate>) (src, type, ctx) -> new JsonPrimitive(src.toString()))
            .registerTypeAdapter(java.time.LocalDateTime.class, (JsonSerializer<java.time.LocalDateTime>) (src, type, ctx) -> new JsonPrimitive(src.toString()))
            .create();
    }

    @Override
    public void afterConnectionEstablished(WebSocketSession session) {
        activeSessions.put(session.getId(), session);
        log.info("New WebSocket connection established: ID={}. Total active clients: {}",
            session.getId(), activeSessions.size());

        // Send immediate comprehensive snapshot so UI renders instantly with zero polling
        sendInitialSnapshot(session);
    }

    @Autowired(required = false)
    private ObjectProvider<com.algotrade.algotradebackend.broker.zerodha.KiteMarketDataService> kiteMarketDataProvider;

    @Override
    protected void handleTextMessage(WebSocketSession session, TextMessage message) {
        String payload = message.getPayload().trim();
        if (payload.contains("PING") || payload.equalsIgnoreCase("ping")) {
            sendSafeMessage(session, "{\"type\":\"PONG\",\"timestamp\":\"" + Instant.now() + "\"}");
            return;
        }

        try {
            com.google.gson.JsonObject json = com.google.gson.JsonParser.parseString(payload).getAsJsonObject();
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
            if (session.isOpen()) {
                synchronized (session) {
                    try {
                        session.sendMessage(textMessage);
                    } catch (IOException e) {
                        log.warn("Failed to send WebSocket frame to {}: {}", session.getId(), e.getMessage());
                    }
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
            snapshotData.put("leaderboard", strategyManager.getLatestTopCandidates());
            snapshotData.put("recentDecisions", activityFeedService.getRecentEvents(50));
            if (scannerProvider != null && scannerProvider.getIfAvailable() != null) {
                snapshotData.put("shortlistedCandidates", scannerProvider.getIfAvailable().getShortlistedCandidates());
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
