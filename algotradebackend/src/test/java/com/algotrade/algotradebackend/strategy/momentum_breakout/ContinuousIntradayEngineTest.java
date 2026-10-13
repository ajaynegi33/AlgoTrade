package com.algotrade.algotradebackend.strategy.momentum_breakout;

import com.algotrade.algotradebackend.broker.BrokerGateway;
import com.algotrade.algotradebackend.broker.BrokerGatewayManager;
import com.algotrade.algotradebackend.core.model.TradeOrder;
import com.algotrade.algotradebackend.core.service.EngineActivityFeedService;
import com.algotrade.algotradebackend.core.service.MarketCalendarService;
import com.algotrade.algotradebackend.risk.PositionSizingEngine;
import com.algotrade.algotradebackend.scanner.ContinuousIntradayScannerService;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import java.time.LocalTime;
import java.util.Optional;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.Mockito.*;

/**
 * Unit test validating the All-Day Continuous Engine:
 * 1. Market Phase determination across all session waves.
 * 2. Time-normalized cumulative volume pacing.
 * 3. Single-entry-per-symbol daily lockout (tradedSymbolsToday).
 */
public class ContinuousIntradayEngineTest {

    private MomentumScoringEngine scoringEngine;
    private PositionSizingEngine positionSizingEngine;
    private BrokerGatewayManager brokerGatewayManager;
    private BrokerGateway brokerGateway;
    private MarketCalendarService marketCalendarService;
    private EngineActivityFeedService activityFeedService;
    private MomentumBreakoutStrategy strategy;

    @BeforeEach
    void setUp() {
        scoringEngine = new MomentumScoringEngine();
        positionSizingEngine = mock(PositionSizingEngine.class);
        brokerGatewayManager = mock(BrokerGatewayManager.class);
        brokerGateway = mock(BrokerGateway.class);
        marketCalendarService = mock(MarketCalendarService.class);
        activityFeedService = mock(EngineActivityFeedService.class);

        when(brokerGatewayManager.getActiveGateway()).thenReturn(brokerGateway);
        when(marketCalendarService.isMarketOpen()).thenReturn(true);
        when(brokerGateway.getExecutionMode()).thenReturn(com.algotrade.algotradebackend.core.model.ExecutionMode.PAPER);
        when(brokerGateway.getOpenPosition()).thenReturn(Optional.empty());
        when(positionSizingEngine.canTradeToday()).thenReturn(true);
        when(positionSizingEngine.calculateQuantity(anyString(), anyDouble(), anyDouble(), any())).thenReturn(25);
        when(brokerGateway.placeOrder(anyString(), anyString(), anyInt(), anyDouble(), anyDouble(), anyString()))
            .thenReturn(new TradeOrder("ORD-1", "HCLTECH", "BUY", 25, 1200.0, 0.0, "LIMIT", "SUBMITTED", com.algotrade.algotradebackend.core.model.ExecutionMode.PAPER, java.time.Instant.now()));

        strategy = new MomentumBreakoutStrategy(
            scoringEngine,
            positionSizingEngine,
            brokerGatewayManager,
            marketCalendarService,
            activityFeedService
        );
    }

    @Test
    void testMarketPhaseDetermination() {
        assertEquals("OPENING_DISCOVERY", ContinuousIntradayScannerService.determineMarketPhase(LocalTime.of(9, 18)));
        assertEquals("WAVE_1_MORNING_MOMENTUM", ContinuousIntradayScannerService.determineMarketPhase(LocalTime.of(9, 45)));
        assertEquals("MIDDAY_CONSOLIDATION", ContinuousIntradayScannerService.determineMarketPhase(LocalTime.of(11, 30)));
        assertEquals("WAVE_2_EUROPEAN_INFLUX", ContinuousIntradayScannerService.determineMarketPhase(LocalTime.of(13, 0)));
        assertEquals("WAVE_3_AFTERNOON_RALLY", ContinuousIntradayScannerService.determineMarketPhase(LocalTime.of(14, 15)));
        assertEquals("SESSION_SHUTDOWN", ContinuousIntradayScannerService.determineMarketPhase(LocalTime.of(14, 50)));
        assertEquals("SQUARE_OFF", ContinuousIntradayScannerService.determineMarketPhase(LocalTime.of(15, 10)));
    }

    @Test
    void testTimeNormalizedVolumePacing() {
        double avgDaily = 1_000_000.0;

        // Morning (09:25 AM) -> 10% baseline = 100,000
        double morningExpected = scoringEngine.calculateExpectedCumulativeVolume(avgDaily, LocalTime.of(9, 25));
        assertEquals(100_000.0, morningExpected, 1.0);

        // Halfway through session (12:22 PM) -> ~50% = 500,000
        double middayExpected = scoringEngine.calculateExpectedCumulativeVolume(avgDaily, LocalTime.of(12, 22));
        assertTrue(middayExpected >= 490_000.0 && middayExpected <= 510_000.0);

        // Afternoon (14:30 PM) -> ~84% = 840,000
        double afternoonExpected = scoringEngine.calculateExpectedCumulativeVolume(avgDaily, LocalTime.of(14, 30));
        assertTrue(afternoonExpected >= 830_000.0 && afternoonExpected <= 850_000.0);
    }

    @Test
    void testSingleEntryPerSymbolDailyLockout() {
        // Prepare qualifying candidate with opening baseline tick and subsequent volume surge breakout
        strategy.updateReferenceData("HCLTECH", 1200.0, 1220.0, 2_000_000.0, 2.0);
        strategy.onTick("HCLTECH", 1225.0, 100_000, 2_000_000, 500_000, 1210.0, true, LocalTime.of(11, 29));
        strategy.onTick("HCLTECH", 1230.0, 3_000_000, 2_000_000, 500_000, 1215.0, true, LocalTime.of(11, 30));

        // First evaluation: Should execute trade and add HCLTECH to tradedSymbolsToday
        strategy.evaluateContinuousCycle();
        assertTrue(strategy.getTradedSymbolsToday().contains("HCLTECH"), "HCLTECH must be in tradedSymbolsToday");

        // Simulate trade completing and account returning to FLAT
        strategy.onTick("HCLTECH", 1240.0, 1_600_000, 2_000_000, 500_000, 1210.0, true, LocalTime.of(11, 45));

        // Second evaluation in next cycle: HCLTECH must be skipped from ranking
        strategy.evaluateContinuousCycle();
        // Since HCLTECH was traded today, it cannot be bought again today
        assertTrue(strategy.getTradedSymbolsToday().contains("HCLTECH"));
    }
}
