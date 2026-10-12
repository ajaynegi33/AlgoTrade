package com.algotrade.algotradebackend.scanner;

import com.algotrade.algotradebackend.persistence.entity.MarketUniverseEntity;
import com.algotrade.algotradebackend.persistence.repository.MarketUniverseRepository;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;

import java.time.Instant;
import java.util.*;

/**
 * Dynamic Registry of liquid National Stock Exchange (NSE) Futures & Options (F&O) underlying stocks.
 * 
 * <p>Zero Hardcoded Lists:
 * <ul>
 *   <li>Dynamically synchronized every morning at 08:30 AM IST from Zerodha Kite Master Instruments.</li>
 *   <li>Only active equity underlyings with live derivative contracts are classified as F&O eligible.</li>
 *   <li>Provides safe fallback to persisted database cache if network is temporarily unreachable.</li>
 * </ul>
 * </p>
 */
@Service
public class FnoUniverseService {

    private static final Logger log = LoggerFactory.getLogger(FnoUniverseService.class);

    private final InstrumentMasterService instrumentMasterService;
    private final MarketUniverseRepository universeRepository;

    public FnoUniverseService(
        InstrumentMasterService instrumentMasterService,
        MarketUniverseRepository universeRepository
    ) {
        this.instrumentMasterService = instrumentMasterService;
        this.universeRepository = universeRepository;
    }

    /**
     * Returns an unmodifiable list of all dynamically discovered active F&O constituent symbols.
     * Consumes the morning validated 08:30 AM database snapshot (0 ms latency at 09:12 AM).
     */
    public List<String> getUniverseSymbols() {
        List<String> cached = instrumentMasterService.getCachedFnoSymbols();
        if (cached != null && !cached.isEmpty()) {
            return cached;
        }

        // Database fallback if in-memory cache is cold
        List<MarketUniverseEntity> entities = universeRepository.findByIsFnoTrueOrderBySymbolAsc();
        if (!entities.isEmpty()) {
            return entities.stream().map(MarketUniverseEntity::getSymbol).toList();
        }

        log.warn("Market universe is empty. Triggering on-demand exchange synchronization...");
        instrumentMasterService.syncMarketUniverse();
        return instrumentMasterService.getCachedFnoSymbols();
    }

    /**
     * Checks whether a symbol belongs to the dynamic liquid F&O universe.
     */
    public boolean isFnoConstituent(String symbol) {
        if (symbol == null || symbol.isBlank()) return false;
        String clean = symbol.trim().toUpperCase().replace("NSE:", "");
        List<String> symbols = getUniverseSymbols();
        return symbols.contains(clean);
    }

    /**
     * Returns all active NSE cash equity symbols in the market.
     */
    public List<String> getAllNseEquitySymbols() {
        return universeRepository.findAllByOrderBySymbolAsc().stream()
            .map(MarketUniverseEntity::getSymbol)
            .toList();
    }

    /**
     * Triggers on-demand exchange master instruments synchronization.
     */
    public Map<String, Object> refreshUniverseFromExchange() {
        return instrumentMasterService.syncMarketUniverse();
    }

    public Instant getLastSyncedAt() {
        return instrumentMasterService.getLastSyncedAt();
    }

    public long getFnoEquitiesCount() {
        return instrumentMasterService.getFnoEquitiesCount();
    }

    public long getTotalEquitiesCount() {
        return instrumentMasterService.getTotalEquitiesCount();
    }
}
