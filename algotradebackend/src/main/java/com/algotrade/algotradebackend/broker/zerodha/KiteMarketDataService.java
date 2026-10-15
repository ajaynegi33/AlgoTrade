package com.algotrade.algotradebackend.broker.zerodha;

import com.algotrade.algotradebackend.core.service.EngineActivityFeedService;
import com.algotrade.algotradebackend.core.service.MarketCalendarService;
import com.algotrade.algotradebackend.strategy.StrategyManager;
import com.algotrade.algotradebackend.strategy.TradingStrategy;
import com.algotrade.algotradebackend.strategy.momentum_breakout.MomentumBreakoutStrategy;
import com.zerodhatech.kiteconnect.KiteConnect;
import com.zerodhatech.kiteconnect.utils.Constants;
import com.zerodhatech.models.OHLCQuote;
import com.zerodhatech.models.Quote;
import com.zerodhatech.models.Tick;
import com.zerodhatech.ticker.KiteTicker;
import com.zerodhatech.ticker.OnConnect;
import com.zerodhatech.ticker.OnDisconnect;
import com.zerodhatech.ticker.OnError;
import com.zerodhatech.ticker.OnTicks;
import jakarta.annotation.PreDestroy;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Service;

import java.util.*;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Real-Time Market Data Ingestion Service.
 * 
 * <p>Bridges the Zerodha Kite WebSocket feed (KiteTicker) and REST endpoints to the trading engine:
 * <ul>
 *   <li><b>09:05 AM IST:</b> Synchronizes daily reference figures (PDH, Previous Close, Avg Volume).</li>
 *   <li><b>09:15:00 AM IST:</b> Captures official opening volume baseline.</li>
 *   <li><b>09:15–15:30 IST:</b> Ingests live ticks and dispatches to {@link StrategyManager#onTick}.</li>
 *   <li><b>09:30:00 AM IST:</b> Triggers the 15-minute momentum scoring evaluation.</li>
 * </ul>
 * </p>
 */
@Service
public class KiteMarketDataService {

    private static final Logger log = LoggerFactory.getLogger(KiteMarketDataService.class);

    private final ZerodhaLiveGateway liveGateway;
    private final StrategyManager strategyManager;
    private final MarketCalendarService marketCalendarService;
    private final EngineActivityFeedService activityFeedService;

    private final Map<Long, String> tokenToSymbolMap = new ConcurrentHashMap<>();
    private final Map<String, Long> symbolToTokenMap = new ConcurrentHashMap<>();
    private volatile KiteTicker kiteTicker = null;
    private volatile boolean connected = false;

    private final java.util.concurrent.atomic.AtomicLong lastTickReceivedMs = new java.util.concurrent.atomic.AtomicLong(System.currentTimeMillis());

    @org.springframework.beans.factory.annotation.Autowired(required = false)
    private com.algotrade.algotradebackend.persistence.repository.MarketUniverseRepository universeRepository;

    @org.springframework.beans.factory.annotation.Autowired(required = false)
    private com.algotrade.algotradebackend.websocket.LiveWebSocketBroadcastService broadcastService;

    @org.springframework.beans.factory.annotation.Autowired(required = false)
    private com.algotrade.algotradebackend.backtest.service.HistoricalMarketDataService historicalDataService;

    // Default static tokens for high-liquidity NSE constituents (fallback if quote API is offline)
    private static final Map<String, Long> DEFAULT_NSE_TOKENS = Map.of(
        "RELIANCE", 738561L,
        "INFY", 408065L,
        "TCS", 2953213L,
        "HDFCBANK", 341249L,
        "ICICIBANK", 1270529L,
        "SBIN", 779521L,
        "BHARTIARTL", 2714625L,
        "LT", 2939649L,
        "AXISBANK", 1510401L,
        "NTPC", 2977281L
    );

    public KiteMarketDataService(
        ZerodhaLiveGateway liveGateway,
        StrategyManager strategyManager,
        MarketCalendarService marketCalendarService,
        EngineActivityFeedService activityFeedService
    ) {
        this.liveGateway = liveGateway;
        this.strategyManager = strategyManager;
        this.marketCalendarService = marketCalendarService;
        this.activityFeedService = activityFeedService;

        // Populate fallback token mappings
        DEFAULT_NSE_TOKENS.forEach((sym, tok) -> {
            tokenToSymbolMap.put(tok, sym);
            symbolToTokenMap.put(sym, tok);
        });
    }

    @jakarta.annotation.PostConstruct
    public void init() {
        if (liveGateway.isSessionActive() && marketCalendarService.isMarketOpen()) {
            startLiveTicker();
        }
    }

    /**
     * Initializes or reconnects the Kite WebSocket stream when session is active.
     */
    public synchronized void startLiveTicker() {
        if (!liveGateway.isSessionActive()) {
            log.warn("Cannot start KiteTicker: Zerodha live session is not authenticated.");
            return;
        }

        String accessToken = liveGateway.getAccessToken();
        String apiKey = liveGateway.getApiKey();

        if (accessToken == null || accessToken.isBlank() || apiKey == null || apiKey.isBlank()) {
            log.warn("Cannot start KiteTicker: Missing accessToken or apiKey.");
            return;
        }

        if (kiteTicker != null && connected) {
            log.info("KiteTicker is already active and connected.");
            return;
        }

        try {
            stopLiveTicker();
            resolveWatchlistTokens();

            log.info("Initializing Zerodha KiteTicker with API Key {}...", apiKey);
            kiteTicker = new KiteTicker(accessToken, apiKey);

            kiteTicker.setOnConnectedListener(new OnConnect() {
                @Override
                public void onConnected() {
                    connected = true;
                    log.info("ZERODHA KITE TICKER CONNECTED! Subscribing to {} constituents...", symbolToTokenMap.size());
                    ArrayList<Long> tokens = new ArrayList<>(symbolToTokenMap.values());
                    if (!tokens.isEmpty()) {
                        kiteTicker.subscribe(tokens);
                        log.info("Subscribed KiteTicker modeFull to {} universe instruments.", tokens.size());
                        log.debug("Subscribed tokens: {}", tokens);
                    }
                    activityFeedService.record(
                        "MARKET_DATA",
                        "INFO",
                        "Live Ticker Connected",
                        "Subscribed to real-time Level-2 tick stream for " + tokens.size() + " symbols."
                    );
                }
            });

            kiteTicker.setOnDisconnectedListener(new OnDisconnect() {
                @Override
                public void onDisconnected() {
                    connected = false;
                    log.warn("Zerodha KiteTicker disconnected.");
                }
            });

            kiteTicker.setOnErrorListener(new OnError() {
                @Override
                public void onError(Exception exception) {
                    log.error("KiteTicker error encountered: {}", exception.getMessage());
                }

                @Override
                public void onError(com.zerodhatech.kiteconnect.kitehttp.exceptions.KiteException kiteException) {
                    log.error("KiteTicker KiteException encountered: {}", kiteException.getMessage());
                }

                @Override
                public void onError(String error) {
                    log.error("KiteTicker error message: {}", error);
                }
            });

            kiteTicker.setOnTickerArrivalListener(new OnTicks() {
                @Override
                public void onTicks(ArrayList<Tick> ticks) {
                    handleIncomingTicks(ticks);
                }
            });

            kiteTicker.setTryReconnection(true);
            kiteTicker.setMaximumRetries(10);
            kiteTicker.setMaximumRetryInterval(30);

            kiteTicker.connect();
        } catch (Throwable t) {
            log.error("Failed to connect KiteTicker: {}", t.getMessage(), t);
        }
    }

    /**
     * Shuts down the KiteTicker WebSocket on application shutdown or mode switch.
     */
    @PreDestroy
    public synchronized void stopLiveTicker() {
        if (kiteTicker != null) {
            try {
                log.info("Disconnecting KiteTicker...");
                kiteTicker.disconnect();
            } catch (Throwable ignored) {}
            kiteTicker = null;
            connected = false;
        }
    }

    /**
     * Resolves instrument tokens dynamically using SQLite repository and KiteConnect getQuote API.
     */
    public synchronized void resolveWatchlistTokens() {
        if (!liveGateway.isSessionActive()) return;
        try {
            // Step 1: Pre-populate from local database cache (all 210 F&O instruments)
            if (universeRepository != null) {
                var fnoList = universeRepository.findByIsFnoTrueOrderBySymbolAsc();
                if (fnoList != null && !fnoList.isEmpty()) {
                    for (var entity : fnoList) {
                        if (entity.getInstrumentToken() != null && entity.getInstrumentToken() > 0) {
                            tokenToSymbolMap.put(entity.getInstrumentToken(), entity.getSymbol());
                            symbolToTokenMap.put(entity.getSymbol(), entity.getInstrumentToken());
                        }
                    }
                    log.info("Loaded {} F&O instrument tokens from local database into active ticker map.", symbolToTokenMap.size());
                }
            }

            // Step 2: Fallback to Kite quote API if database is incomplete
            if (symbolToTokenMap.size() < 20) {
                KiteConnect client = liveGateway.getKiteConnect();
                Set<String> symbols = getWatchlistSymbols();
                if (symbols != null && !symbols.isEmpty()) {
                    String[] keys = symbols.stream().map(s -> "NSE:" + s).toArray(String[]::new);
                    Map<String, Quote> quotes = client.getQuote(keys);

                    for (Map.Entry<String, Quote> entry : quotes.entrySet()) {
                        String cleanSym = entry.getKey().replace("NSE:", "");
                        Quote q = entry.getValue();
                        if (q != null && q.instrumentToken > 0) {
                            tokenToSymbolMap.put(q.instrumentToken, cleanSym);
                            symbolToTokenMap.put(cleanSym, q.instrumentToken);
                        }
                    }
                }
            }

            log.info("Resolved {} watchlist symbols to Kite instrument tokens.", symbolToTokenMap.size());

            // Step 3: If KiteTicker is active and connected, subscribe all 210 tokens in modeFull
            if (kiteTicker != null && connected && !symbolToTokenMap.isEmpty()) {
                ArrayList<Long> tokens = new ArrayList<>(symbolToTokenMap.values());
                kiteTicker.subscribe(tokens);
                kiteTicker.setMode(tokens, KiteTicker.modeFull);
                log.info("Subscribed KiteTicker modeFull to {} dynamic universe tokens.", tokens.size());
            }
        } catch (Throwable t) {
            log.warn("Dynamic token resolution encountered error: {}. Using fallback mappings.", t.getMessage());
        }
    }

    /**
     * Dynamically registers and subscribes a specific stock symbol to the live KiteTicker stream.
     */
    public synchronized void subscribeSymbol(String symbol) {
        if (symbol == null || symbol.isBlank() || !liveGateway.isSessionActive()) return;
        String cleanSym = symbol.trim().toUpperCase().replace("NSE:", "");
        if (symbolToTokenMap.containsKey(cleanSym)) {
            Long token = symbolToTokenMap.get(cleanSym);
            if (kiteTicker != null && connected && token != null) {
                ArrayList<Long> list = new ArrayList<>(List.of(token));
                kiteTicker.subscribe(list);
                kiteTicker.setMode(list, KiteTicker.modeFull);
            }
            return;
        }

        try {
            KiteConnect client = liveGateway.getKiteConnect();
            if (client == null) return;
            Map<String, Quote> quotes = client.getQuote(new String[]{"NSE:" + cleanSym});
            Quote q = (quotes != null) ? quotes.get("NSE:" + cleanSym) : null;
            if (q != null && q.instrumentToken > 0) {
                tokenToSymbolMap.put(q.instrumentToken, cleanSym);
                symbolToTokenMap.put(cleanSym, q.instrumentToken);
                if (kiteTicker != null && connected) {
                    ArrayList<Long> list = new ArrayList<>(List.of(q.instrumentToken));
                    kiteTicker.subscribe(list);
                    kiteTicker.setMode(list, KiteTicker.modeFull);
                    log.info("Dynamically subscribed KiteTicker to searched symbol: {} (token={})", cleanSym, q.instrumentToken);
                }
            }
        } catch (Throwable t) {
            log.warn("Failed to subscribe symbol {} to KiteTicker: {}", cleanSym, t.getMessage());
        }
    }

    private void handleIncomingTicks(List<Tick> ticks) {
        if (ticks == null || ticks.isEmpty()) return;
        lastTickReceivedMs.set(System.currentTimeMillis());

        for (Tick tick : ticks) {
            String symbol = tokenToSymbolMap.get(tick.getInstrumentToken());
            if (symbol == null) continue;

            double ltp = tick.getLastTradedPrice();
            long volume = tick.getVolumeTradedToday();
            long buyQty = (long) tick.getTotalBuyQuantity();
            long sellQty = (long) tick.getTotalSellQuantity();
            double changePct = tick.getChange();
            double prevClose = tick.getClosePrice();
            double change = prevClose > 0 ? (ltp - prevClose) : (ltp * (changePct / 100.0));
            double high = tick.getHighPrice();
            double low = tick.getLowPrice();
            double vwap = tick.getAverageTradePrice();

            // Extract 5-level market depth from Tick
            Map<String, Object> depthMap = null;
            if (tick.getMarketDepth() != null && !tick.getMarketDepth().isEmpty()) {
                var depthObj = tick.getMarketDepth();
                depthMap = new LinkedHashMap<>();
                if (depthObj.containsKey("buy") && depthObj.get("buy") != null) {
                    List<Map<String, Object>> buyList = new ArrayList<>();
                    for (com.zerodhatech.models.Depth d : depthObj.get("buy")) {
                        Map<String, Object> item = new LinkedHashMap<>();
                        item.put("price", d.getPrice());
                        item.put("quantity", d.getQuantity());
                        item.put("orders", d.getOrders());
                        buyList.add(item);
                    }
                    depthMap.put("buy", buyList);
                }
                if (depthObj.containsKey("sell") && depthObj.get("sell") != null) {
                    List<Map<String, Object>> sellList = new ArrayList<>();
                    for (com.zerodhatech.models.Depth d : depthObj.get("sell")) {
                        Map<String, Object> item = new LinkedHashMap<>();
                        item.put("price", d.getPrice());
                        item.put("quantity", d.getQuantity());
                        item.put("orders", d.getOrders());
                        sellList.add(item);
                    }
                    depthMap.put("sell", sellList);
                }
            }

            if (ltp > 0) {
                strategyManager.onTick(symbol, ltp, volume, buyQty, sellQty, vwap);

                if (broadcastService != null) {
                    double open = tick.getOpenPrice();
                    int ltq = (int) tick.getLastTradedQuantity();
                    String ltt = tick.getLastTradedTime() != null ? tick.getLastTradedTime().toString() : null;

                    broadcastService.broadcastTick(
                        symbol,
                        ltp,
                        volume,
                        Math.round(change * 100.0) / 100.0,
                        Math.round(changePct * 100.0) / 100.0,
                        high,
                        low,
                        buyQty,
                        sellQty,
                        depthMap,
                        open,
                        prevClose,
                        vwap,
                        ltq,
                        ltt
                    );
                }
            }
        }
    }

    /**
     * Pre-market Reference Data Sync: 09:05 AM IST, Monday through Friday.
     * Fetches previous close and previous day high for the active watchlist.
     */
    @Scheduled(cron = "0 5 9 * * MON-FRI", zone = "Asia/Kolkata")
    public void scheduledPreMarketSync() {
        java.time.LocalDate today = java.time.LocalDate.now(java.time.ZoneId.of("Asia/Kolkata"));
        if (!marketCalendarService.isTradingDay(today) || !liveGateway.isSessionActive()) {
            return;
        }
        log.info("=== RUNNING PRE-MARKET REFERENCE DATA SYNC (09:05 AM IST) ===");
        try {
            TradingStrategy strategy = strategyManager.getActiveStrategy();
            if (!(strategy instanceof MomentumBreakoutStrategy mbStrategy)) {
                return;
            }

            Set<String> symbols = getWatchlistSymbols();
            KiteConnect client = liveGateway.getKiteConnect();
            if (client == null || !liveGateway.isSessionActive()) {
                log.warn("Zerodha session not authenticated for pre-market sync.");
                return;
            }

            String[] keys = symbols.stream().map(s -> "NSE:" + s).toArray(String[]::new);
            Map<String, OHLCQuote> ohlcMap = client.getOHLC(keys);

            for (String sym : symbols) {
                OHLCQuote q = ohlcMap.get("NSE:" + sym);
                double prevClose = (q != null && q.ohlc != null && q.ohlc.close > 0) ? q.ohlc.close : 0.0;
                double pdh = 0.0;
                double avgVol = 2_000_000.0; // Baseline fallback
                double atrPct = 2.0;

                if (historicalDataService != null) {
                    try {
                        var baseline = historicalDataService.getBaselineReference(sym, today);
                        if (baseline != null) {
                            if (prevClose <= 0) prevClose = baseline.prevClose();
                            pdh = baseline.pdh();
                            avgVol = baseline.avgDailyVol();
                            atrPct = baseline.atrPct();
                        }
                    } catch (Throwable t) {
                        log.warn("Historical baseline lookup failed for {}: {}", sym, t.getMessage());
                    }
                }

                if (pdh <= 0 && q != null && q.ohlc != null) {
                    pdh = q.ohlc.high > 0 ? q.ohlc.high : prevClose;
                }

                if (prevClose > 0 || pdh > 0) {
                    mbStrategy.updateReferenceData(sym, prevClose, pdh, avgVol, atrPct);
                    log.info("Populated reference data for {}: PrevClose=₹{}, PDH=₹{}, ADV={}, ATR={}%", sym, prevClose, pdh, avgVol, atrPct);
                }
            }

            // Ensure live ticker is connected ahead of 09:15 open
            startLiveTicker();
        } catch (Throwable t) {
            log.error("Failed to sync pre-market reference figures: {}", t.getMessage());
        }
    }

    /**
     * 09:15:00 AM IST: Sets the opening volume baseline for the session.
     */
    @Scheduled(cron = "0 15 9 * * MON-FRI", zone = "Asia/Kolkata")
    public void scheduledOpeningVolumeBaseline() {
        if (!liveGateway.isSessionActive()) return;
        log.info("=== CAPTURING 09:15:00 OPENING VOLUME BASELINE ===");
        try {
            TradingStrategy strategy = strategyManager.getActiveStrategy();
            if (strategy instanceof MomentumBreakoutStrategy mbStrategy) {
                KiteConnect client = liveGateway.getKiteConnect();
                Set<String> symbols = getWatchlistSymbols();
                String[] keys = symbols.stream().map(s -> "NSE:" + s).toArray(String[]::new);
                Map<String, Quote> quotes = client.getQuote(keys);

                for (String sym : symbols) {
                    Quote q = quotes.get("NSE:" + sym);
                    if (q != null) {
                        mbStrategy.setOpeningVolumeBaseline(sym, (long) q.volumeTradedToday);
                    }
                }
            }
        } catch (Throwable t) {
            log.warn("Failed to capture 09:15:00 volume baseline: {}", t.getMessage());
        }
    }

    /**
     * 09:30:00 AM IST: Runs the official Momentum Breakout candidate evaluation.
     */
    @Scheduled(cron = "0 30 9 * * MON-FRI", zone = "Asia/Kolkata")
    public void scheduled0930Evaluation() {
        log.info("=== TRIGGERING 09:30:00 AM STRATEGY EVALUATION ===");
        strategyManager.evaluate0930();
    }

    private Set<String> getWatchlistSymbols() {
        if (!symbolToTokenMap.isEmpty()) {
            return symbolToTokenMap.keySet();
        }
        if (universeRepository != null) {
            try {
                var list = universeRepository.findByIsFnoTrueOrderBySymbolAsc();
                if (list != null && !list.isEmpty()) {
                    Set<String> set = new HashSet<>();
                    for (var e : list) set.add(e.getSymbol());
                    return set;
                }
            } catch (Throwable ignored) {}
        }
        TradingStrategy strategy = strategyManager.getActiveStrategy();
        if (strategy instanceof MomentumBreakoutStrategy mbStrategy) {
            return mbStrategy.getMetricsMap().keySet();
        }
        return DEFAULT_NSE_TOKENS.keySet();
    }

    public synchronized void reconnectTicker() {
        log.warn("Reconnecting KiteTicker on demand/watchdog trigger...");
        stopLiveTicker();
        startLiveTicker();
    }

    public long getLastTickReceivedMs() {
        return lastTickReceivedMs.get();
    }

    public boolean isConnected() {
        return connected;
    }
}
