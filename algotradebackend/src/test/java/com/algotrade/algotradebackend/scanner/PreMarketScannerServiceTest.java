package com.algotrade.algotradebackend.scanner;

import com.algotrade.algotradebackend.broker.zerodha.ZerodhaLiveGateway;
import com.algotrade.algotradebackend.core.service.MarketCalendarService;
import com.algotrade.algotradebackend.core.service.EngineActivityFeedService;
import com.algotrade.algotradebackend.persistence.entity.PreMarketCandidateEntity;
import com.algotrade.algotradebackend.persistence.repository.PreMarketCandidateRepository;
import com.algotrade.algotradebackend.strategy.StrategyManager;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.mockito.Mockito;

import java.time.LocalDate;
import java.util.List;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.when;

class PreMarketScannerServiceTest {

    private FnoUniverseService universeService;
    private ZerodhaLiveGateway liveGateway;
    private PreMarketCandidateRepository candidateRepository;
    private StrategyManager strategyManager;
    private MarketCalendarService marketCalendarService;
    private EngineActivityFeedService activityFeedService;
    private PreMarketScannerService scannerService;

    @BeforeEach
    void setUp() {
        InstrumentMasterService instrumentMasterService = Mockito.mock(InstrumentMasterService.class);
        com.algotrade.algotradebackend.persistence.repository.MarketUniverseRepository universeRepository = Mockito.mock(com.algotrade.algotradebackend.persistence.repository.MarketUniverseRepository.class);
        List<String> mockFnoList = List.of(
            "RELIANCE", "TCS", "INFY", "HDFCBANK", "ICICIBANK", "SBIN", "BHARTIARTL", "LT", "AXISBANK", "TATASTEEL"
        );
        when(instrumentMasterService.getCachedFnoSymbols()).thenReturn(mockFnoList);

        universeService = new FnoUniverseService(instrumentMasterService, universeRepository);
        liveGateway = Mockito.mock(ZerodhaLiveGateway.class);
        candidateRepository = Mockito.mock(PreMarketCandidateRepository.class);
        strategyManager = Mockito.mock(StrategyManager.class);
        marketCalendarService = Mockito.mock(MarketCalendarService.class);
        activityFeedService = Mockito.mock(EngineActivityFeedService.class);

        when(marketCalendarService.isMarketOpen()).thenReturn(true);
        when(liveGateway.isSessionActive()).thenReturn(false); // Testing simulated pre-open fallback

        scannerService = new PreMarketScannerService(
            universeService,
            liveGateway,
            candidateRepository,
            strategyManager,
            marketCalendarService,
            activityFeedService
        );
    }

    @Test
    void testFnoUniverseServiceIntegrity() {
        List<String> symbols = universeService.getUniverseSymbols();
        assertNotNull(symbols);
        assertFalse(symbols.isEmpty(), "F&O universe must contain constituents");
        assertTrue(universeService.isFnoConstituent("RELIANCE"));
        assertTrue(universeService.isFnoConstituent("NSE:TATASTEEL"));
        assertFalse(universeService.isFnoConstituent("UNKNOWN_PENNY_STOCK"));
    }

    @Test
    void testPreMarketScannerExecutionAndFiltering() {
        List<PreMarketCandidateEntity> results = scannerService.executeScan();

        assertNotNull(results);
        assertFalse(results.isEmpty(), "Scanner must produce evaluated candidates");

        // Verify ranks are sequentially assigned 1 to N
        for (int i = 0; i < results.size(); i++) {
            assertEquals(i + 1, results.get(i).getRankOrder(), "Rank order must be contiguous 1-indexed");
        }

        // Check shortlisted candidates
        List<PreMarketCandidateEntity> shortlisted = results.stream()
            .filter(c -> "SHORTLISTED".equals(c.getStatus()))
            .toList();

        // Shortlisted candidates must be <= max configured (6)
        assertTrue(shortlisted.size() <= 6, "Must not shortlist more than max candidates (6)");

        // Every shortlisted candidate must satisfy gap [+0.60%, +2.50%]
        for (PreMarketCandidateEntity c : shortlisted) {
            assertTrue(c.getGapPct() >= 0.60 && c.getGapPct() <= 2.50,
                "Shortlisted candidate gap must be within [+0.60%, +2.50%]: " + c.getSymbol() + " was " + c.getGapPct());
            assertNull(c.getRejectionReason(), "Shortlisted candidate must not have a rejection reason");
            assertNotNull(c.getRankingScore());
            assertTrue(c.getRankingScore() > 0);
        }

        // Check rejected candidates have explicit diagnostic reasons
        List<PreMarketCandidateEntity> rejected = results.stream()
            .filter(c -> "REJECTED".equals(c.getStatus()))
            .toList();

        for (PreMarketCandidateEntity r : rejected) {
            assertNotNull(r.getRejectionReason(), "Rejected candidates must have an informative rejection reason");
            assertTrue(
                r.getRejectionReason().contains("GAP_TOO_LOW") ||
                r.getRejectionReason().contains("GAP_TOO_HIGH") ||
                r.getRejectionReason().contains("LOW_PRE_OPEN_VOLUME") ||
                r.getRejectionReason().contains("CIRCUIT_HEADROOM_INSUFFICIENT"),
                "Rejection reason must match one of the quantitative gates: " + r.getRejectionReason()
            );
        }
    }
}
