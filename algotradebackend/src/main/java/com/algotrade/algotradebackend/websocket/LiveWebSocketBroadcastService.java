package com.algotrade.algotradebackend.websocket;

import com.algotrade.algotradebackend.core.model.EngineActivityEvent;
import com.google.gson.Gson;
import com.google.gson.GsonBuilder;
import com.google.gson.JsonPrimitive;
import com.google.gson.JsonSerializer;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;

import java.time.Instant;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * High-performance broadcasting service that pushes engine events in real-time
 * to all connected browser clients over the active WebSocket stream.
 */
@Service
public class LiveWebSocketBroadcastService {

    private static final Logger log = LoggerFactory.getLogger(LiveWebSocketBroadcastService.class);

    private final LiveEngineWebSocketHandler handler;
    private final Gson gson;

    public LiveWebSocketBroadcastService(LiveEngineWebSocketHandler handler) {
        this.handler = handler;
        this.gson = new GsonBuilder()
            .registerTypeAdapter(Instant.class, (JsonSerializer<Instant>) (src, type, ctx) -> new JsonPrimitive(src.toString()))
            .registerTypeAdapter(java.time.LocalDate.class, (JsonSerializer<java.time.LocalDate>) (src, type, ctx) -> new JsonPrimitive(src.toString()))
            .registerTypeAdapter(java.time.LocalDateTime.class, (JsonSerializer<java.time.LocalDateTime>) (src, type, ctx) -> new JsonPrimitive(src.toString()))
            .create();
    }

    /**
     * Broadcasts a typed JSON payload frame to all active WebSocket clients.
     * 
     * @param type Event type identifier (e.g. "DECISION_EVENT", "POSITION_UPDATE")
     * @param data Payload data object
     */
    public void broadcast(String type, Object data) {
        if (handler.getActiveSessionCount() == 0) {
            return; // No connected clients, avoid serialization overhead
        }

        try {
            Map<String, Object> frame = new LinkedHashMap<>();
            frame.put("type", type);
            frame.put("timestamp", Instant.now().toString());
            frame.put("data", data);

            String json = gson.toJson(frame);
            handler.broadcast(json);
        } catch (Throwable t) {
            log.warn("Failed to broadcast WebSocket frame of type {}: {}", type, t.getMessage());
        }
    }

    /**
     * Broadcasts live engine overview update (capital, status, active position).
     */
    public void broadcastOverview() {
        broadcast("OVERVIEW_UPDATE", handler.buildOverviewData());
    }

    /**
     * Broadcasts an individual decision feed log event as it occurs.
     */
    public void broadcastDecisionEvent(EngineActivityEvent event) {
        broadcast("DECISION_EVENT", event);
    }

    /**
     * Broadcasts a position change or PnL update.
     */
    public void broadcastPositionUpdate(Object position) {
        broadcast("POSITION_UPDATE", position);
    }

    /**
     * Broadcasts updated scoring leaderboard rankings.
     */
    public void broadcastLeaderboardUpdate(Object leaderboard) {
        broadcast("LEADERBOARD_UPDATE", leaderboard);
    }

    /**
     * Broadcasts updated pre-market scanner results.
     */
    public void broadcastScannerUpdate(Object scannerData) {
        broadcast("SCANNER_UPDATE", scannerData);
    }

    /**
     * Broadcasts real-time price tick for a symbol to active clients.
     */
    public void broadcastTick(String symbol, double ltp, long volume, double change, double changePct, double high, double low) {
        broadcastTick(symbol, ltp, volume, change, changePct, high, low, 0L, 0L, null);
    }

    /**
     * Broadcasts real-time price tick along with Level-2 market depth and total order book quantities.
     */
    public void broadcastTick(
        String symbol,
        double ltp,
        long volume,
        double change,
        double changePct,
        double high,
        double low,
        long totalBuyQuantity,
        long totalSellQuantity,
        Map<String, Object> depth
    ) {
        broadcastTick(symbol, ltp, volume, change, changePct, high, low, totalBuyQuantity, totalSellQuantity, depth, 0.0, 0.0, 0.0, 0, null);
    }

    /**
     * Broadcasts comprehensive real-time price tick with full market metrics, depth, and timestamps.
     */
    public void broadcastTick(
        String symbol,
        double ltp,
        long volume,
        double change,
        double changePct,
        double high,
        double low,
        long totalBuyQuantity,
        long totalSellQuantity,
        Map<String, Object> depth,
        double open,
        double close,
        double averagePrice,
        int lastTradedQuantity,
        String lastTradedTime
    ) {
        Map<String, Object> tick = new LinkedHashMap<>();
        tick.put("symbol", symbol);
        tick.put("ltp", ltp);
        tick.put("volume", volume);
        tick.put("change", change);
        tick.put("changePct", changePct);
        tick.put("high", high);
        tick.put("low", low);
        if (open > 0) tick.put("open", open);
        if (close > 0) tick.put("close", close);
        if (averagePrice > 0) tick.put("averagePrice", averagePrice);
        if (lastTradedQuantity > 0) tick.put("lastTradedQuantity", lastTradedQuantity);
        if (lastTradedTime != null && !lastTradedTime.isBlank()) tick.put("lastTradedTime", lastTradedTime);
        tick.put("totalBuyQuantity", totalBuyQuantity);
        tick.put("totalSellQuantity", totalSellQuantity);
        if (depth != null && !depth.isEmpty()) {
            tick.put("depth", depth);
        }
        double total = (double) totalBuyQuantity + (double) totalSellQuantity;
        if (total > 0) {
            double buyDepthPct = ((double) totalBuyQuantity / total) * 100.0;
            tick.put("buyDepthRatio", Math.round(buyDepthPct * 10.0) / 10.0);
        }
        broadcast("TICK_UPDATE", tick);
    }
}
