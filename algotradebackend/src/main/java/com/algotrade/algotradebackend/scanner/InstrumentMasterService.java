package com.algotrade.algotradebackend.scanner;

import com.algotrade.algotradebackend.core.service.EngineActivityFeedService;
import com.algotrade.algotradebackend.persistence.entity.MarketUniverseEntity;
import com.algotrade.algotradebackend.persistence.repository.MarketUniverseRepository;
import jakarta.annotation.PostConstruct;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import java.io.BufferedReader;
import java.io.StringReader;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Duration;
import java.time.Instant;
import java.util.*;
import java.util.concurrent.CopyOnWriteArrayList;

/**
 * Dynamic Exchange Master Instruments Synchronization Service.
 * 
 * <p>Downloads, validates, and stores the live NSE &amp; NFO market universe:
 * <ul>
 *   <li><b>08:30:00 AM IST:</b> Synchronizes daily instruments master from Kite endpoint.</li>
 *   <li><b>Cross-References NFO vs NSE Cash:</b> Identifies equity underlyings with active derivatives.</li>
 *   <li><b>Transactional Cache:</b> Persists validated records to SQLite table 'market_universe'.</li>
 *   <li><b>0 ms Pre-Open Latency:</b> Scanner reads from local verified snapshot at 09:12 AM.</li>
 * </ul>
 * </p>
 */
@Service
public class InstrumentMasterService {

    private static final Logger log = LoggerFactory.getLogger(InstrumentMasterService.class);

    private static final String KITE_NSE_INSTRUMENTS_URL = "https://api.kite.trade/instruments/NSE";
    private static final String KITE_NFO_INSTRUMENTS_URL = "https://api.kite.trade/instruments/NFO";

    private static final Set<String> EXCLUDED_DERIVATIVE_UNDERLYINGS = Set.of(
        "NIFTY", "BANKNIFTY", "FINNIFTY", "MIDCPNIFTY", "NIFTYNXT50"
    );

    private final MarketUniverseRepository universeRepository;
    private final EngineActivityFeedService activityFeedService;
    private final HttpClient httpClient;

    @Value("${algotrade.scanner.universe.auto-sync-on-startup:true}")
    private boolean autoSyncOnStartup;

    private final List<String> cachedFnoSymbols = new CopyOnWriteArrayList<>();
    private volatile Instant lastSyncedAt = null;

    public InstrumentMasterService(
        MarketUniverseRepository universeRepository,
        EngineActivityFeedService activityFeedService
    ) {
        this.universeRepository = universeRepository;
        this.activityFeedService = activityFeedService;
        this.httpClient = HttpClient.newBuilder()
            .connectTimeout(Duration.ofSeconds(10))
            .build();
    }

    @PostConstruct
    public void init() {
        // Load existing database records into memory cache
        loadCacheFromDatabase();

        // If database is empty or auto-sync enabled, trigger synchronization
        if (cachedFnoSymbols.isEmpty() && autoSyncOnStartup) {
            log.info("Market universe is empty in database. Triggering initial exchange synchronization...");
            new Thread(this::syncMarketUniverse, "universe-initial-sync").start();
        }
    }

    /**
     * Daily scheduled sync at 08:30:00 AM IST (Monday to Friday).
     * Follows Zerodha exchange publication schedule.
     */
    @Scheduled(cron = "${algotrade.scanner.universe.sync-cron:0 30 8 * * MON-FRI}", zone = "Asia/Kolkata")
    public void scheduledMorningSync() {
        log.info("=== RUNNING SCHEDULED 08:30 AM EXCHANGE INSTRUMENTS SYNCHRONIZATION ===");
        syncMarketUniverse();
    }

    /**
     * Executes the dynamic exchange instruments retrieval, schema validation, and persistence.
     * 
     * @return Summary of synchronization results
     */
    public synchronized Map<String, Object> syncMarketUniverse() {
        Instant startTime = Instant.now();
        log.info("Starting live market universe sync from Kite endpoints...");

        try {
            // Step 1: Download NFO instruments dump to extract active derivatives underlyings
            String nfoCsv = downloadCsv(KITE_NFO_INSTRUMENTS_URL);
            Set<String> activeFnoUnderlyings = extractFnoUnderlyings(nfoCsv);
            log.info("Discovered {} unique active underlying names from NFO derivatives stream.", activeFnoUnderlyings.size());

            // Step 2: Download NSE cash instruments dump
            String nseCsv = downloadCsv(KITE_NSE_INSTRUMENTS_URL);
            List<MarketUniverseEntity> validatedEntities = extractAndCrossReferenceNseEquities(nseCsv, activeFnoUnderlyings);
            log.info("Validated {} total NSE equity instruments ({} qualified F&O underlyings).",
                validatedEntities.size(),
                validatedEntities.stream().filter(MarketUniverseEntity::isFno).count());

            if (validatedEntities.isEmpty()) {
                log.warn("Exchange sync returned 0 valid instruments. Preserving previous database cache.");
                return Map.of("success", false, "message", "Downloaded data contained 0 valid equity instruments.");
            }

            // Step 3: Transactional Persistence
            persistUniverse(validatedEntities);

            // Step 4: Refresh In-Memory Cache
            loadCacheFromDatabase();
            lastSyncedAt = Instant.now();

            long fnoCount = cachedFnoSymbols.size();
            long totalCount = validatedEntities.size();
            long durationMs = Duration.between(startTime, lastSyncedAt).toMillis();

            log.info("MARKET UNIVERSE SYNC COMPLETED in {} ms: {} F&O underlyings, {} total equities.",
                durationMs, fnoCount, totalCount);

            activityFeedService.record(
                "UNIVERSE_SYNC",
                "SUCCESS",
                "Market Universe Synchronized",
                String.format("Synchronized live exchange master: %d liquid F&O stocks discovered across %d active NSE equities.",
                    fnoCount, totalCount),
                null,
                Map.of("fnoCount", fnoCount, "totalEquities", totalCount, "durationMs", durationMs)
            );

            Map<String, Object> result = new LinkedHashMap<>();
            result.put("success", true);
            result.put("fnoCount", fnoCount);
            result.put("totalCount", totalCount);
            result.put("durationMs", durationMs);
            result.put("lastSyncedAt", lastSyncedAt);
            result.put("sampleFnoSymbols", cachedFnoSymbols.stream().limit(10).toList());
            return result;

        } catch (Throwable t) {
            log.error("Failed to sync market universe from exchange: {}", t.getMessage(), t);
            activityFeedService.record(
                "UNIVERSE_SYNC",
                "WARN",
                "Market Universe Sync Failed",
                "Exchange sync error: " + t.getMessage() + ". Retaining previous cached universe."
            );
            return Map.of("success", false, "error", t.getMessage(), "fnoCount", cachedFnoSymbols.size());
        }
    }

    /**
     * Extracts active equity underlyings from NFO derivatives CSV.
     */
    private Set<String> extractFnoUnderlyings(String csvData) {
        Set<String> underlyings = new TreeSet<>();
        if (csvData == null || csvData.isBlank()) return underlyings;

        try (BufferedReader reader = new BufferedReader(new StringReader(csvData))) {
            String header = reader.readLine();
            if (header == null) return underlyings;

            String line;
            while ((line = reader.readLine()) != null) {
                List<String> row = parseCsvRow(line);
                if (row.size() < 12) continue;

                // Index 9: instrument_type ("FUT"), Index 10: segment ("NFO-FUT"), Index 3: name
                String instrumentType = row.get(9);
                String segment = row.get(10);
                String name = row.get(3).replace("\"", "").trim().toUpperCase();

                if ("FUT".equalsIgnoreCase(instrumentType) && "NFO-FUT".equalsIgnoreCase(segment)) {
                    if (!name.isEmpty() && !EXCLUDED_DERIVATIVE_UNDERLYINGS.contains(name)) {
                        underlyings.add(name);
                    }
                }
            }
        } catch (Throwable t) {
            log.warn("Error parsing NFO CSV: {}", t.getMessage());
        }
        return underlyings;
    }

    /**
     * Extracts active NSE cash equities and cross-references them against active F&O underlyings.
     */
    private List<MarketUniverseEntity> extractAndCrossReferenceNseEquities(String csvData, Set<String> activeFnoUnderlyings) {
        List<MarketUniverseEntity> list = new ArrayList<>();
        if (csvData == null || csvData.isBlank()) return list;

        Instant now = Instant.now();
        Set<String> seenSymbols = new HashSet<>();

        try (BufferedReader reader = new BufferedReader(new StringReader(csvData))) {
            String header = reader.readLine();
            if (header == null) return list;

            String line;
            while ((line = reader.readLine()) != null) {
                List<String> row = parseCsvRow(line);
                if (row.size() < 12) continue;

                // Index 0: instrument_token, Index 2: tradingsymbol, Index 3: name
                // Index 7: tick_size, Index 8: lot_size, Index 9: instrument_type ("EQ"), Index 10: segment ("NSE"), Index 11: exchange ("NSE")
                String symbol = row.get(2).replace("\"", "").trim().toUpperCase();
                String companyName = row.get(3).replace("\"", "").trim();
                String instrumentType = row.get(9);
                String segment = row.get(10);
                String exchange = row.get(11);

                if ("EQ".equalsIgnoreCase(instrumentType) && "NSE".equalsIgnoreCase(segment) && "NSE".equalsIgnoreCase(exchange)) {
                    // Filter out debt securities, series with hyphens (e.g. bonds), and duplicates
                    if (symbol.contains("-") || symbol.matches(".*\\d{3,}.*")) {
                        continue;
                    }

                    if (!seenSymbols.add(symbol)) {
                        continue;
                    }

                    long token = parseLong(row.get(0));
                    double tickSize = parseDouble(row.get(7), 0.05);
                    int lotSize = parseInt(row.get(8), 1);

                    boolean isFno = activeFnoUnderlyings.contains(symbol);

                    MarketUniverseEntity entity = new MarketUniverseEntity(
                        symbol,
                        companyName,
                        exchange,
                        segment,
                        token,
                        isFno,
                        lotSize,
                        tickSize,
                        now
                    );
                    list.add(entity);
                }
            }
        } catch (Throwable t) {
            log.warn("Error parsing NSE CSV: {}", t.getMessage());
        }

        return list;
    }

    @Transactional
    public void persistUniverse(List<MarketUniverseEntity> entities) {
        universeRepository.deleteAll();
        universeRepository.saveAll(entities);
        log.info("Persisted {} market universe entities into database.", entities.size());
    }

    private void loadCacheFromDatabase() {
        List<MarketUniverseEntity> fnoEntities = universeRepository.findByIsFnoTrueOrderBySymbolAsc();
        List<String> symbols = fnoEntities.stream()
            .map(MarketUniverseEntity::getSymbol)
            .toList();

        cachedFnoSymbols.clear();
        cachedFnoSymbols.addAll(symbols);

        if (!fnoEntities.isEmpty()) {
            lastSyncedAt = fnoEntities.get(0).getLastSyncedAt();
        }

        log.info("Loaded {} F&O universe symbols into active cache from database.", cachedFnoSymbols.size());
    }

    private String downloadCsv(String url) throws Exception {
        HttpRequest request = HttpRequest.newBuilder()
            .uri(URI.create(url))
            .timeout(Duration.ofSeconds(15))
            .header("User-Agent", "AlgoTrade-Engine/1.0")
            .GET()
            .build();

        HttpResponse<String> response = httpClient.send(request, HttpResponse.BodyHandlers.ofString());
        if (response.statusCode() != 200) {
            throw new RuntimeException("HTTP " + response.statusCode() + " downloading " + url);
        }
        return response.body();
    }

    /**
     * CSV line parser with quotation handling.
     */
    public static List<String> parseCsvRow(String line) {
        List<String> tokens = new ArrayList<>();
        StringBuilder sb = new StringBuilder();
        boolean inQuotes = false;
        for (int i = 0; i < line.length(); i++) {
            char c = line.charAt(i);
            if (c == '"') {
                inQuotes = !inQuotes;
            } else if (c == ',' && !inQuotes) {
                tokens.add(sb.toString().trim());
                sb.setLength(0);
            } else {
                sb.append(c);
            }
        }
        tokens.add(sb.toString().trim());
        return tokens;
    }

    private long parseLong(String val) {
        try { return Long.parseLong(val.replace("\"", "").trim()); } catch (Throwable ignored) { return 0L; }
    }

    private int parseInt(String val, int def) {
        try { return Integer.parseInt(val.replace("\"", "").trim()); } catch (Throwable ignored) { return def; }
    }

    private double parseDouble(String val, double def) {
        try { return Double.parseDouble(val.replace("\"", "").trim()); } catch (Throwable ignored) { return def; }
    }

    public List<String> getCachedFnoSymbols() {
        return Collections.unmodifiableList(cachedFnoSymbols);
    }

    public Instant getLastSyncedAt() {
        return lastSyncedAt;
    }

    public long getTotalEquitiesCount() {
        return universeRepository.count();
    }

    public long getFnoEquitiesCount() {
        return cachedFnoSymbols.size();
    }
}
