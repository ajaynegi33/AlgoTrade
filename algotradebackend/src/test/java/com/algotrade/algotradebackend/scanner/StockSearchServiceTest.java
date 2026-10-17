package com.algotrade.algotradebackend.scanner;

import com.algotrade.algotradebackend.persistence.entity.MarketUniverseEntity;
import com.algotrade.algotradebackend.persistence.repository.MarketUniverseRepository;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.mockito.Mockito;

import java.time.Instant;
import java.util.List;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.Mockito.when;

class StockSearchServiceTest {

    private MarketUniverseRepository universeRepository;
    private StockSearchService searchService;

    @BeforeEach
    void setUp() {
        universeRepository = Mockito.mock(MarketUniverseRepository.class);

        Instant now = Instant.now();
        List<MarketUniverseEntity> sampleEntities = List.of(
            new MarketUniverseEntity("RELIANCE", "RELIANCE INDUSTRIES LIMITED", "NSE", "NSE", 738561L, true, 1, 0.05, now),
            new MarketUniverseEntity("TCS", "TATA CONSULTANCY SERVICES LIMITED", "NSE", "NSE", 2953217L, true, 1, 0.05, now),
            new MarketUniverseEntity("INFY", "INFOSYS LIMITED", "NSE", "NSE", 408065L, true, 1, 0.05, now),
            new MarketUniverseEntity("SBIN", "STATE BANK OF INDIA", "NSE", "NSE", 779521L, true, 1, 0.05, now),
            new MarketUniverseEntity("TATAMOTORS", "TATA MOTORS LIMITED", "NSE", "NSE", 884737L, true, 1, 0.05, now),
            new MarketUniverseEntity("TATASTEEL", "TATA STEEL LIMITED", "NSE", "NSE", 895745L, true, 1, 0.05, now),
            new MarketUniverseEntity("LT", "LARSEN & TOUBRO LIMITED", "NSE", "NSE", 2939649L, true, 1, 0.05, now),
            new MarketUniverseEntity("M&M", "MAHINDRA & MAHINDRA LIMITED", "NSE", "NSE", 519937L, true, 1, 0.05, now),
            new MarketUniverseEntity("BHARTIARTL", "BHARTI AIRTEL LIMITED", "NSE", "NSE", 2714625L, true, 1, 0.05, now),
            new MarketUniverseEntity("MARUTI", "MARUTI SUZUKI INDIA LIMITED", "NSE", "NSE", 2815745L, true, 1, 0.05, now),
            new MarketUniverseEntity("TATAPENNY", "TATA PENNY CASH ILLIQUID", "NSE", "NSE", 999999L, false, 1, 0.05, now)
        );

        when(universeRepository.findAllByOrderBySymbolAsc()).thenReturn(sampleEntities);

        searchService = new StockSearchService(universeRepository);
        searchService.init();
    }

    @Test
    void testExactSymbolSearch() {
        List<StockSearchResult> results = searchService.search("SBIN", 5);
        assertFalse(results.isEmpty());
        assertEquals("SBIN", results.get(0).symbol());
        assertEquals("STATE BANK OF INDIA", results.get(0).companyName());
    }

    @Test
    void testCompanyNameSearch() {
        // User searches for "State Bank of India" -> Should return SBIN as #1
        List<StockSearchResult> results = searchService.search("State Bank", 5);
        assertFalse(results.isEmpty());
        assertEquals("SBIN", results.get(0).symbol());

        // User searches for "Infosys" -> Should return INFY
        List<StockSearchResult> infyResults = searchService.search("Infosys", 5);
        assertFalse(infyResults.isEmpty());
        assertEquals("INFY", infyResults.get(0).symbol());
    }

    @Test
    void testCommonIndianMarketAliases() {
        // User searches "SBI" -> Ticker is SBIN
        List<StockSearchResult> sbiResults = searchService.search("SBI", 5);
        assertFalse(sbiResults.isEmpty());
        assertEquals("SBIN", sbiResults.get(0).symbol());

        // User searches "L&T" or "Larsen" -> Ticker is LT
        List<StockSearchResult> ltResults = searchService.search("L&T", 5);
        assertFalse(ltResults.isEmpty());
        assertEquals("LT", ltResults.get(0).symbol());

        // User searches "Airtel" -> Ticker is BHARTIARTL
        List<StockSearchResult> airtelResults = searchService.search("Airtel", 5);
        assertFalse(airtelResults.isEmpty());
        assertEquals("BHARTIARTL", airtelResults.get(0).symbol());

        // User searches "M&M" or "Mahindra" -> Ticker is M&M
        List<StockSearchResult> mmResults = searchService.search("mm", 5);
        assertFalse(mmResults.isEmpty());
        assertEquals("M&M", mmResults.get(0).symbol());
    }

    @Test
    void testPunctuationAndWhitespaceNormalization() {
        // "Tata Motors" (with space) should match TATAMOTORS
        List<StockSearchResult> results = searchService.search("Tata Motors", 5);
        assertFalse(results.isEmpty());
        assertEquals("TATAMOTORS", results.get(0).symbol());

        // "tatamotors" (all lowercase without space) should match
        List<StockSearchResult> resultsNoSpace = searchService.search("tatamotors", 5);
        assertFalse(resultsNoSpace.isEmpty());
        assertEquals("TATAMOTORS", resultsNoSpace.get(0).symbol());
    }

    @Test
    void testFnoLiquidityBoost() {
        // Both TATAMOTORS (isFno=true) and TATAPENNY (isFno=false) start with TATA.
        // F&O stock should rank higher than non-F&O.
        List<StockSearchResult> results = searchService.search("TATA", 5);
        assertFalse(results.isEmpty());

        int fnoIndex = -1;
        int nonFnoIndex = -1;
        for (int i = 0; i < results.size(); i++) {
            if ("TATAMOTORS".equals(results.get(i).symbol())) fnoIndex = i;
            if ("TATAPENNY".equals(results.get(i).symbol())) nonFnoIndex = i;
        }

        assertTrue(fnoIndex >= 0 && nonFnoIndex >= 0);
        assertTrue(fnoIndex < nonFnoIndex, "F&O stock should rank higher than non-F&O penny stock");
    }

    @Test
    void testEmptyAndBlankQueries() {
        assertTrue(searchService.search("", 5).isEmpty());
        assertTrue(searchService.search("   ", 5).isEmpty());
        assertTrue(searchService.search(null, 5).isEmpty());
    }

    @Test
    void testResultLimit() {
        List<StockSearchResult> results = searchService.search("TATA", 2);
        assertTrue(results.size() <= 2);
    }
}
