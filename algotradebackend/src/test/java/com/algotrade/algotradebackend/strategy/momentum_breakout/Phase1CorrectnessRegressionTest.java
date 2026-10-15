package com.algotrade.algotradebackend.strategy.momentum_breakout;

import com.algotrade.algotradebackend.broker.BrokerGateway;
import com.algotrade.algotradebackend.broker.BrokerGatewayManager;
import com.algotrade.algotradebackend.core.model.ExecutionMode;
import com.algotrade.algotradebackend.core.model.TradeOrder;
import com.algotrade.algotradebackend.core.service.EngineActivityFeedService;
import com.algotrade.algotradebackend.core.service.MarketCalendarService;
import com.algotrade.algotradebackend.risk.PositionSizingEngine;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import java.time.Instant;
import java.time.LocalTime;
import java.util.Optional;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.Mockito.*;

/**
 * Runnable regression suite verifying the 5 Phase 1 correctness & performance fixes:
 * 1. PDH is preserved and not overwritten by intraday highs.
 * 2. low15 and high15 are locked strictly to the 09:15-09:30 observation window.
 * 3. Backtest typical-price VWAP is not corrupted by bodyImbalance.
 * 4. In-memory guards reject non-breakouts without calling REST gateway or database.
 * 5. Runner-up candidate is evaluated and executed when Rank 1 fails sizing/support gates.
 */
public class Phase1CorrectnessRegressionTest {

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
        when(brokerGateway.getExecutionMode()).thenReturn(ExecutionMode.PAPER);
        when(brokerGateway.getOpenPosition()).thenReturn(Optional.empty());
        when(positionSizingEngine.canTradeToday()).thenReturn(true);
        when(positionSizingEngine.calculateQuantity(anyString(), anyDouble(), anyDouble(), any())).thenReturn(50);
        when(brokerGateway.placeOrder(anyString(), anyString(), anyInt(), anyDouble(), anyDouble(), anyString()))
            .thenReturn(new TradeOrder("ORD-REG-1", "INFY", "BUY", 50, 1628.0, 0.0, "LIMIT", "SUBMITTED", ExecutionMode.PAPER, Instant.now()));

        strategy = new MomentumBreakoutStrategy(
            scoringEngine,
            positionSizingEngine,
            brokerGatewayManager,
            marketCalendarService,
            activityFeedService
        );
    }

    @Test
    void test1_PdhNotOverwrittenByIntradayHigh() {
        CandidateMetrics m = new CandidateMetrics("RELIANCE", 1000.0, 1010.0, 5_000_000, 2.0);

        // Attempt to clobber PDH with today's intraday high of 1050.0
        m.setReferenceData(1000.0, 1050.0, 2_000_000, 2.0);

        assertEquals(1010.0, m.getPreviousDayHigh(), 0.001,
            "PDH must remain anchored to the established 1010.0 and reject intraday overwrite");
    }

    @Test
    void test2_ObservationWindowLocksLow15AndHigh15() {
        CandidateMetrics m = new CandidateMetrics("TCS", 3500.0, 3550.0, 2_000_000, 1.5);

        // Ticks during 09:15 - 09:30 observation window
        m.updateTick(3520.0, 50_000, 10_000, 5_000, 0.0, true);
        m.updateTick(3540.0, 80_000, 15_000, 5_000, 0.0, true);

        assertEquals(3520.0, m.getLow15(), 0.001);
        assertEquals(3540.0, m.getHigh15(), 0.001);

        // Ticks AFTER observation window (inObservationWindow = false)
        m.updateTick(3480.0, 120_000, 20_000, 5_000, 0.0, false); // Lower price
        m.updateTick(3570.0, 150_000, 25_000, 5_000, 0.0, false); // Higher price

        assertEquals(3520.0, m.getLow15(), 0.001, "Low15 must remain strictly locked to 09:15-09:30 window");
        assertEquals(3540.0, m.getHigh15(), 0.001, "High15 must remain strictly locked to 09:15-09:30 window");
        assertEquals(3570.0, m.getLtp(), 0.001, "LTP must update to the latest tick");
    }

    @Test
    void test3_BacktestVwapDoesNotUseBodyImbalance() {
        CandidateMetrics m = new CandidateMetrics("INFY", 1500.0, 1520.0, 5_000_000, 2.0);

        // Aggregate 5-minute candle: Open=1505, High=1515, Low=1500, Close=1510, Vol=100,000
        // Typical price = (1515 + 1500 + 1510) / 3 = 1508.33
        m.aggregateCandle(1505.0, 1515.0, 1500.0, 1510.0, 100_000, true);

        // Pass 0.0 for tickExchangeVwap (the fix for backtest passing bodyImbalance)
        m.updateTick(1510.0, 100_000, 60_000, 40_000, 0.0, true);

        double vwap = m.getVwap();
        assertEquals(1508.333, vwap, 0.01, "VWAP must be accurately accumulated from candle typical prices");

        // Scoring engine evaluates VWAP distance: vdPct = (1510 - 1508.333) / 1508.333 * 100 = ~0.11%
        var snapshot = m.getSnapshot();
        var scored = scoringEngine.evaluate(snapshot, LocalTime.of(9, 25));

        assertTrue(scored.vwapScore() > 0, "Valid VWAP expansion must receive a positive score (not 0)");
    }

    @Test
    void test4_BreakoutCheckReordersGuardsBeforeGateway() {
        // Warmup tick initializes daily session once
        strategy.onTick("WARMUP", 100.0, 1000, 500, 500, 0.0, false, LocalTime.of(9, 16));
        clearInvocations(brokerGateway, positionSizingEngine);

        strategy.updateReferenceData("SBIN", 800.0, 810.0, 10_000_000, 2.0);

        // Incoming tick below PDH (805.0 < 810.0)
        strategy.onTick("SBIN", 805.0, 500_000, 100_000, 50_000, 0.0, false, LocalTime.of(10, 0));

        // Broker gateway position and daily trade repository queries must NOT be called for non-breakouts
        verify(brokerGateway, never()).getOpenPosition();
        verify(positionSizingEngine, never()).canTradeToday();
    }

    @Test
    void test5_RunnerUpSelectedWhenWinnerFailsSizing() {
        // Setup Candidate 1: HCLTECH (Rank 1 score, but support distance is 3.25% > 1.2% cap)
        strategy.updateReferenceData("HCLTECH", 1200.0, 1220.0, 2_000_000.0, 2.0);
        strategy.onTick("HCLTECH", 1190.0, 100_000, 2_000_000, 500_000, 1210.0, true, LocalTime.of(9, 20)); // Low locked at 1190.0
        strategy.onTick("HCLTECH", 1230.0, 3_000_000, 2_000_000, 500_000, 1215.0, false, LocalTime.of(11, 30)); // Entry 1230 -> Dist = (1230-1190)/1230 = 3.25% (overextended)

        // Setup Candidate 2: INFY (Rank 2 score, valid support distance of 0.49%)
        strategy.updateReferenceData("INFY", 1600.0, 1610.0, 2_000_000.0, 2.0);
        strategy.onTick("INFY", 1620.0, 100_000, 2_000_000, 500_000, 1612.0, true, LocalTime.of(9, 20)); // Low locked at 1620.0
        strategy.onTick("INFY", 1628.0, 3_000_000, 2_000_000, 500_000, 1615.0, false, LocalTime.of(11, 30)); // Entry 1628 -> Dist = (1628-1620)/1628 = 0.49% (valid)

        // Trigger continuous evaluation cycle
        strategy.evaluateContinuousCycle();

        // INFY must have an order placed because HCLTECH was overextended!
        verify(brokerGateway, times(1)).placeOrder(
            eq("INFY"), eq("BUY"), eq(50), anyDouble(), eq(0.0), eq("LIMIT")
        );
        verify(brokerGateway, never()).placeOrder(
            eq("HCLTECH"), anyString(), anyInt(), anyDouble(), anyDouble(), anyString()
        );
    }
}
