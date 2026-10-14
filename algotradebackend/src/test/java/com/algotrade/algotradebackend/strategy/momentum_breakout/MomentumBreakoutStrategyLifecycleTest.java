package com.algotrade.algotradebackend.strategy.momentum_breakout;

import com.algotrade.algotradebackend.broker.BrokerGateway;
import com.algotrade.algotradebackend.broker.BrokerGatewayManager;
import com.algotrade.algotradebackend.core.model.ExecutionMode;
import com.algotrade.algotradebackend.core.model.Position;
import com.algotrade.algotradebackend.core.model.TradeOrder;
import com.algotrade.algotradebackend.core.service.EngineActivityFeedService;
import com.algotrade.algotradebackend.core.service.MarketCalendarService;
import com.algotrade.algotradebackend.risk.PositionSizingEngine;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import java.time.Instant;
import java.util.List;
import java.util.Optional;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.ArgumentMatchers.*;
import static org.mockito.Mockito.*;

public class MomentumBreakoutStrategyLifecycleTest {

    private MomentumScoringEngine scoringEngine;
    private PositionSizingEngine positionSizingEngine;
    private BrokerGatewayManager brokerGatewayManager;
    private BrokerGateway mockGateway;
    private MarketCalendarService marketCalendarService;
    private EngineActivityFeedService activityFeedService;
    private MomentumBreakoutStrategy strategy;

    @BeforeEach
    void setUp() {
        scoringEngine = new MomentumScoringEngine();
        positionSizingEngine = mock(PositionSizingEngine.class);
        brokerGatewayManager = mock(BrokerGatewayManager.class);
        mockGateway = mock(BrokerGateway.class);
        marketCalendarService = mock(MarketCalendarService.class);
        activityFeedService = new EngineActivityFeedService();

        when(brokerGatewayManager.getActiveGateway()).thenReturn(mockGateway);
        when(marketCalendarService.isMarketOpen()).thenReturn(true);
        when(marketCalendarService.isObservationWindow()).thenReturn(true);
        when(mockGateway.getExecutionMode()).thenReturn(ExecutionMode.PAPER);

        strategy = new MomentumBreakoutStrategy(
            scoringEngine,
            positionSizingEngine,
            brokerGatewayManager,
            marketCalendarService,
            activityFeedService
        );
    }

    @Test
    void testPendingEntryDoesNotAbandonTradeOnTicks() {
        // Setup a qualifying candidate
        strategy.updateReferenceData("RELIANCE", 1000.0, 1010.0, 10_000_000, 2.5);
        strategy.onTick("RELIANCE", 1005.0, 100_000, 2_000_000, 500_000);
        strategy.onTick("RELIANCE", 1015.0, 3_000_000, 2_000_000, 500_000); // Breaks PDH (1010)
        strategy.onTick("RELIANCE", 1020.0, 3_100_000, 2_000_000, 500_000);

        when(positionSizingEngine.canTradeToday()).thenReturn(true);
        when(positionSizingEngine.calculateQuantity(eq("RELIANCE"), anyDouble(), anyDouble(), any())).thenReturn(10);
        when(mockGateway.placeOrder(eq("RELIANCE"), eq("BUY"), eq(10), anyDouble(), eq(0.0), eq("LIMIT")))
            .thenReturn(new TradeOrder("ORD-BUY-1", "RELIANCE", "BUY", 10, 1020.0, 0.0, "LIMIT", "SUBMITTED", ExecutionMode.PAPER, Instant.now()));

        // In live trading, buy order is submitted, but not yet filled when evaluate0930 finishes
        when(mockGateway.getOpenPosition()).thenReturn(Optional.empty());

        strategy.evaluate0930();

        assertEquals(TradeLifecycleState.PENDING_ENTRY, strategy.getLifecycleState());
        assertEquals("RELIANCE", strategy.getActiveSymbol());

        // A tick arrives while buy order is still pending at the exchange
        // This previously triggered the else branch and cleared activeSymbol!
        strategy.onTick("RELIANCE", 1021.0, 3_150_000, 2_000_000, 500_000);

        // Verify activeSymbol is NOT abandoned
        assertEquals(TradeLifecycleState.PENDING_ENTRY, strategy.getLifecycleState(), "Strategy must stay in PENDING_ENTRY");
        assertEquals("RELIANCE", strategy.getActiveSymbol(), "activeSymbol must NOT be cleared while order is pending");

        // Now simulate order fill confirmed by broker
        Position filledPosition = new Position("RELIANCE", 10, 1020.0, 1021.0, 1004.95, 1004.95, 1021.0, Instant.now(), ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition()).thenReturn(Optional.of(filledPosition));
        when(mockGateway.placeOrder(eq("RELIANCE"), eq("SELL"), eq(10), anyDouble(), anyDouble(), eq("SL")))
            .thenReturn(new TradeOrder("ORD-SL-1", "RELIANCE", "SELL", 10, 1002.9, 1004.95, "SL", "TRIGGER PENDING", ExecutionMode.PAPER, Instant.now()));

        // Next tick triggers fill confirmation and places protective SL
        strategy.onTick("RELIANCE", 1022.0, 3_200_000, 2_000_000, 500_000);

        assertEquals(TradeLifecycleState.ACTIVE_POSITION, strategy.getLifecycleState());
        verify(mockGateway, times(1)).placeOrder(eq("RELIANCE"), eq("SELL"), eq(10), anyDouble(), anyDouble(), eq("SL"));
        assertTrue(strategy.getStrategyHighWaterMark() >= 1022.0, "Persistent High-Water Mark must track highest price");
    }

    @Test
    void testTrailingStopRatchetsWithPersistentHwmAfter11AM() {
        // Setup active position directly
        strategy.updateReferenceData("RELIANCE", 1000.0, 1010.0, 10_000_000, 2.5);
        strategy.onTick("RELIANCE", 1005.0, 100_000, 2_000_000, 500_000);
        strategy.onTick("RELIANCE", 1015.0, 3_000_000, 2_000_000, 500_000);
        strategy.onTick("RELIANCE", 1020.0, 3_100_000, 2_000_000, 500_000);

        when(positionSizingEngine.canTradeToday()).thenReturn(true);
        when(positionSizingEngine.calculateQuantity(eq("RELIANCE"), anyDouble(), anyDouble(), any())).thenReturn(10);
        when(mockGateway.placeOrder(eq("RELIANCE"), eq("BUY"), anyInt(), anyDouble(), anyDouble(), eq("LIMIT")))
            .thenReturn(new TradeOrder("ORD-BUY-1", "RELIANCE", "BUY", 10, 1020.0, 0.0, "LIMIT", "FILLED", ExecutionMode.PAPER, Instant.now()));
        when(mockGateway.placeOrder(eq("RELIANCE"), eq("SELL"), anyInt(), anyDouble(), anyDouble(), eq("SL")))
            .thenReturn(new TradeOrder("ORD-SL-1", "RELIANCE", "SELL", 10, 1002.9, 1004.95, "SL", "TRIGGER PENDING", ExecutionMode.PAPER, Instant.now()));

        // Pre-entry: empty. Post-entry: initPos
        Position initPos = new Position("RELIANCE", 10, 1020.0, 1020.0, 1004.95, 1004.95, 1020.0, Instant.now(), ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition())
            .thenReturn(Optional.empty())       // At evaluate0930 start
            .thenReturn(Optional.of(initPos));  // Immediate fill check

        strategy.evaluate0930();
        assertEquals(TradeLifecycleState.ACTIVE_POSITION, strategy.getLifecycleState());

        // Price rises to 1050.0 at 11:30 AM
        Position peakPos = new Position("RELIANCE", 10, 1020.0, 1050.0, 1004.95, 1004.95, 1050.0, Instant.now(), ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition()).thenReturn(Optional.of(peakPos));

        java.time.LocalTime time1130 = java.time.LocalTime.of(11, 30);
        strategy.onTick("RELIANCE", 1050.0, 4_000_000, 2_000_000, 500_000, false, time1130);

        // Trail stop is 0.8% below 1050 = 1050 * (1 - 0.008) = 1041.60
        verify(mockGateway, atLeastOnce()).modifyOrder(eq("ORD-SL-1"), anyDouble(), eq(1041.60));
        assertEquals(1050.0, strategy.getStrategyHighWaterMark(), "Strategy HWM must be 1050.0");

        // Price pulls back to 1035.0 at 11:45 AM.
        // Even if broker's getOpenPosition lastPrice is 1035, HWM must stay 1050.0!
        Position pullbackPos = new Position("RELIANCE", 10, 1020.0, 1035.0, 1004.95, 1041.60, 1035.0, Instant.now(), ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition()).thenReturn(Optional.of(pullbackPos));

        strategy.onTick("RELIANCE", 1035.0, 4_200_000, 2_000_000, 500_000, false, java.time.LocalTime.of(11, 45));
        assertEquals(1050.0, strategy.getStrategyHighWaterMark(), "HWM must NOT decrease on pullback!");
    }

    @Test
    void testBreakevenLockTriggerAtOnePercentGain() {
        // Setup active position at 1020.0 entry with 1004.95 initial stop
        strategy.updateReferenceData("RELIANCE", 1000.0, 1010.0, 10_000_000, 2.5);
        strategy.onTick("RELIANCE", 1005.0, 100_000, 2_000_000, 500_000);
        strategy.onTick("RELIANCE", 1015.0, 3_000_000, 2_000_000, 500_000);
        strategy.onTick("RELIANCE", 1020.0, 3_100_000, 2_000_000, 500_000);

        when(positionSizingEngine.canTradeToday()).thenReturn(true);
        when(positionSizingEngine.calculateQuantity(eq("RELIANCE"), anyDouble(), anyDouble(), any())).thenReturn(10);
        when(mockGateway.placeOrder(eq("RELIANCE"), eq("BUY"), anyInt(), anyDouble(), anyDouble(), eq("LIMIT")))
            .thenReturn(new TradeOrder("ORD-BUY-1", "RELIANCE", "BUY", 10, 1020.0, 0.0, "LIMIT", "FILLED", ExecutionMode.PAPER, Instant.now()));
        when(mockGateway.placeOrder(eq("RELIANCE"), eq("SELL"), anyInt(), anyDouble(), anyDouble(), eq("SL")))
            .thenReturn(new TradeOrder("ORD-SL-1", "RELIANCE", "SELL", 10, 1002.9, 1004.95, "SL", "TRIGGER PENDING", ExecutionMode.PAPER, Instant.now()));

        Position initPos = new Position("RELIANCE", 10, 1020.0, 1020.0, 1004.95, 1004.95, 1020.0, Instant.now(), ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition())
            .thenReturn(Optional.empty())
            .thenReturn(Optional.of(initPos));

        strategy.evaluate0930();
        assertEquals(TradeLifecycleState.ACTIVE_POSITION, strategy.getLifecycleState());

        // Price rises to 1031.0 (+1.08% gain) at 10:15 AM (Before 11:00 AM trailing window)
        Position gainPos = new Position("RELIANCE", 10, 1020.0, 1031.0, 1004.95, 1004.95, 1031.0, Instant.now(), ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition()).thenReturn(Optional.of(gainPos));

        java.time.LocalTime time1015 = java.time.LocalTime.of(10, 15);
        strategy.onTick("RELIANCE", 1031.0, 3_500_000, 2_000_000, 500_000, false, time1015);

        // Exact Solvency Breakeven target guarantees Net PnL >= +₹10.00 after all charges (1022.10)
        verify(mockGateway, times(1)).modifyOrder(eq("ORD-SL-1"), anyDouble(), eq(1022.10));
    }

    @Test
    void testSquareOffCancelsSlAndFlattensPosition() {
        strategy.updateReferenceData("RELIANCE", 1000.0, 1010.0, 10_000_000, 2.5);
        strategy.onTick("RELIANCE", 1005.0, 100_000, 2_000_000, 500_000);
        strategy.onTick("RELIANCE", 1015.0, 3_000_000, 2_000_000, 500_000);
        strategy.onTick("RELIANCE", 1020.0, 3_100_000, 2_000_000, 500_000);

        when(positionSizingEngine.canTradeToday()).thenReturn(true);
        when(positionSizingEngine.calculateQuantity(eq("RELIANCE"), anyDouble(), anyDouble(), any())).thenReturn(10);
        when(mockGateway.placeOrder(eq("RELIANCE"), eq("BUY"), anyInt(), anyDouble(), anyDouble(), eq("LIMIT")))
            .thenReturn(new TradeOrder("ORD-BUY-1", "RELIANCE", "BUY", 10, 1020.0, 0.0, "LIMIT", "FILLED", ExecutionMode.PAPER, Instant.now()));
        when(mockGateway.placeOrder(eq("RELIANCE"), eq("SELL"), anyInt(), anyDouble(), anyDouble(), eq("SL")))
            .thenReturn(new TradeOrder("ORD-SL-1", "RELIANCE", "SELL", 10, 1002.9, 1004.95, "SL", "TRIGGER PENDING", ExecutionMode.PAPER, Instant.now()));

        Position pos = new Position("RELIANCE", 10, 1020.0, 1025.0, 1004.95, 1004.95, 1025.0, Instant.now(), ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition())
            .thenReturn(Optional.empty())       // At evaluate0930 start
            .thenReturn(Optional.of(pos));      // After fill

        strategy.evaluate0930();
        assertEquals(TradeLifecycleState.ACTIVE_POSITION, strategy.getLifecycleState());

        // Now trigger 15:05 square-off
        when(mockGateway.placeOrder(eq("RELIANCE"), eq("SELL"), eq(10), anyDouble(), eq(0.0), eq("LIMIT")))
            .thenReturn(new TradeOrder("ORD-EXIT-1", "RELIANCE", "SELL", 10, 1023.95, 0.0, "LIMIT", "FILLED", ExecutionMode.PAPER, Instant.now()));
        // Broker confirms position flat after exit
        when(mockGateway.getOpenPosition())
            .thenReturn(Optional.of(pos))  // First read in square-off
            .thenReturn(Optional.empty()); // Check after exit

        strategy.executeSquareOff();

        verify(mockGateway, times(1)).cancelOrder("ORD-SL-1");
        verify(mockGateway, times(1)).placeOrder(eq("RELIANCE"), eq("SELL"), eq(10), anyDouble(), eq(0.0), eq("LIMIT"));
        assertEquals(TradeLifecycleState.FLAT, strategy.getLifecycleState());
        assertNull(strategy.getActiveSymbol());
    }

    @Test
    void testSquareOffCancelsPendingEntryOrder() {
        strategy.updateReferenceData("RELIANCE", 1000.0, 1010.0, 10_000_000, 2.5);
        strategy.onTick("RELIANCE", 1005.0, 100_000, 2_000_000, 500_000);
        strategy.onTick("RELIANCE", 1015.0, 3_000_000, 2_000_000, 500_000);
        strategy.onTick("RELIANCE", 1020.0, 3_100_000, 2_000_000, 500_000);

        when(positionSizingEngine.canTradeToday()).thenReturn(true);
        when(positionSizingEngine.calculateQuantity(eq("RELIANCE"), anyDouble(), anyDouble(), any())).thenReturn(10);
        when(mockGateway.placeOrder(eq("RELIANCE"), eq("BUY"), anyInt(), anyDouble(), anyDouble(), eq("LIMIT")))
            .thenReturn(new TradeOrder("ORD-BUY-1", "RELIANCE", "BUY", 10, 1020.0, 0.0, "LIMIT", "SUBMITTED", ExecutionMode.PAPER, Instant.now()));

        // Entry is submitted, but remains unfilled throughout the session
        when(mockGateway.getOpenPosition()).thenReturn(Optional.empty());

        strategy.evaluate0930();
        assertEquals(TradeLifecycleState.PENDING_ENTRY, strategy.getLifecycleState());

        // At 15:05 cutoff, square-off triggers
        strategy.executeSquareOff();

        // Must cancel the resting buy order to prevent late accidental fill
        verify(mockGateway, times(1)).cancelOrder("ORD-BUY-1");
        assertEquals(TradeLifecycleState.FLAT, strategy.getLifecycleState());
        assertNull(strategy.getActiveSymbol());
    }

    @Test
    void testSquareOffReconcilesPartialFillAfterCancellingPendingEntry() {
        strategy.updateReferenceData("RELIANCE", 1000.0, 1010.0, 10_000_000, 2.5);
        strategy.onTick("RELIANCE", 1005.0, 100_000, 2_000_000, 500_000);
        strategy.onTick("RELIANCE", 1015.0, 3_000_000, 2_000_000, 500_000);
        strategy.onTick("RELIANCE", 1020.0, 3_100_000, 2_000_000, 500_000);

        when(positionSizingEngine.canTradeToday()).thenReturn(true);
        when(positionSizingEngine.calculateQuantity(eq("RELIANCE"), anyDouble(), anyDouble(), any())).thenReturn(10);
        when(mockGateway.placeOrder(eq("RELIANCE"), eq("BUY"), anyInt(), anyDouble(), anyDouble(), eq("LIMIT")))
            .thenReturn(new TradeOrder("ORD-BUY-1", "RELIANCE", "BUY", 10, 1020.0, 0.0, "LIMIT", "SUBMITTED", ExecutionMode.PAPER, Instant.now()));

        when(mockGateway.getOpenPosition()).thenReturn(Optional.empty());
        strategy.evaluate0930();
        assertEquals(TradeLifecycleState.PENDING_ENTRY, strategy.getLifecycleState());

        // At 15:05, entry order had partially filled 4 shares!
        Position partialPos = new Position("RELIANCE", 4, 1020.0, 1022.0, 1004.95, 1004.95, 1022.0, Instant.now(), ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition())
            .thenReturn(Optional.of(partialPos))  // Found partial position during square-off reconciliation
            .thenReturn(Optional.empty());        // Flat after limit exit

        when(mockGateway.placeOrder(eq("RELIANCE"), eq("SELL"), eq(4), anyDouble(), eq(0.0), eq("LIMIT")))
            .thenReturn(new TradeOrder("ORD-EXIT-1", "RELIANCE", "SELL", 4, 1021.0, 0.0, "LIMIT", "FILLED", ExecutionMode.PAPER, Instant.now()));

        strategy.executeSquareOff();

        // Must cancel the resting unfilled entry order portion
        verify(mockGateway, times(1)).cancelOrder("ORD-BUY-1");
        // Must immediately liquidate the 4 filled shares!
        verify(mockGateway, times(1)).placeOrder(eq("RELIANCE"), eq("SELL"), eq(4), anyDouble(), eq(0.0), eq("LIMIT"));
        assertEquals(TradeLifecycleState.FLAT, strategy.getLifecycleState());
    }

    @Test
    void testFailedStopPlacementRetriesOnNextTickWithoutLeavingPositionUnprotected() {
        strategy.updateReferenceData("RELIANCE", 1000.0, 1010.0, 10_000_000, 2.5);
        strategy.onTick("RELIANCE", 1005.0, 100_000, 2_000_000, 500_000);
        strategy.onTick("RELIANCE", 1015.0, 3_000_000, 2_000_000, 500_000);
        strategy.onTick("RELIANCE", 1020.0, 3_100_000, 2_000_000, 500_000);

        when(positionSizingEngine.canTradeToday()).thenReturn(true);
        when(positionSizingEngine.calculateQuantity(eq("RELIANCE"), anyDouble(), anyDouble(), any())).thenReturn(10);
        when(mockGateway.placeOrder(eq("RELIANCE"), eq("BUY"), anyInt(), anyDouble(), anyDouble(), eq("LIMIT")))
            .thenReturn(new TradeOrder("ORD-BUY-1", "RELIANCE", "BUY", 10, 1020.0, 0.0, "LIMIT", "SUBMITTED", ExecutionMode.PAPER, Instant.now()));
        when(mockGateway.getOpenPosition()).thenReturn(Optional.empty());

        strategy.evaluate0930();
        assertEquals(TradeLifecycleState.PENDING_ENTRY, strategy.getLifecycleState());

        Position filledPos = new Position("RELIANCE", 10, 1020.0, 1021.0, 1004.95, 1004.95, 1021.0, Instant.now(), ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition()).thenReturn(Optional.of(filledPos));

        // First attempt to place stop-loss fails due to transient broker error
        when(mockGateway.placeOrder(eq("RELIANCE"), eq("SELL"), eq(10), anyDouble(), anyDouble(), eq("SL")))
            .thenThrow(new RuntimeException("Broker Network Spike"))
            .thenReturn(new TradeOrder("ORD-SL-1", "RELIANCE", "SELL", 10, 1002.9, 1004.95, "SL", "TRIGGER PENDING", ExecutionMode.PAPER, Instant.now()));

        // Tick 1 arrives: stop placement throws, so state remains PENDING_ENTRY (not blindly ACTIVE_POSITION)
        strategy.onTick("RELIANCE", 1021.0, 3_200_000, 2_000_000, 500_000);
        assertEquals(TradeLifecycleState.PENDING_ENTRY, strategy.getLifecycleState(), "State must remain PENDING_ENTRY if SL placement fails");

        // Tick 2 arrives: retries placing stop-loss, succeeds, now transitions to ACTIVE_POSITION
        strategy.onTick("RELIANCE", 1022.0, 3_250_000, 2_000_000, 500_000);
        assertEquals(TradeLifecycleState.ACTIVE_POSITION, strategy.getLifecycleState(), "State must transition to ACTIVE_POSITION once SL is armed");
        verify(mockGateway, times(2)).placeOrder(eq("RELIANCE"), eq("SELL"), eq(10), anyDouble(), anyDouble(), eq("SL"));
    }

    @Test
    void testSessionResetWithCarriedOverPositionSetsDateWithoutLoop() {
        Position openPos = new Position("INFY", 50, 1600.0, 1610.0, 1585.0, 1585.0, 1610.0, Instant.now(), ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition()).thenReturn(Optional.of(openPos));

        // Call reset session
        strategy.resetDailySession();

        assertEquals(TradeLifecycleState.ACTIVE_POSITION, strategy.getLifecycleState());
        assertEquals("INFY", strategy.getActiveSymbol());

        // Now dispatch a tick for INFY. If lastSessionDate wasn't set, onTick would call ensureSessionCurrent()
        // and invoke resetDailySession() again, wiping CandidateMetrics.
        strategy.updateReferenceData("INFY", 1590.0, 1620.0, 5_000_000, 2.0);
        strategy.onTick("INFY", 1612.0, 100_000, 500_000, 200_000);

        // Verify high water mark is updated and active symbol remains INFY
        assertEquals("INFY", strategy.getActiveSymbol());
        assertEquals(1612.0, strategy.getStrategyHighWaterMark());
        // Verify getOpenPosition was called during initial reset and during tick lifecycle management, but NOT in a reset loop
        verify(mockGateway, atMost(3)).getOpenPosition();
    }

    @Test
    void testSetPreMarketWatchlistFocusesStrategyPool() {
        // Initially default watchlist has 10 symbols
        assertEquals(10, strategy.getMetricsMap().size());

        // Dynamic 09:12 scan provides top 3 shortlisted candidates
        List<String> dynamicList = List.of("TCS", "ESCORTS", "ASHOKLEY");
        strategy.setPreMarketWatchlist(dynamicList);

        // Active metrics pool must now focus exclusively on the dynamic shortlisted candidates
        assertEquals(3, strategy.getMetricsMap().size());
        assertTrue(strategy.getMetricsMap().containsKey("TCS"));
        assertTrue(strategy.getMetricsMap().containsKey("ESCORTS"));
        assertTrue(strategy.getMetricsMap().containsKey("ASHOKLEY"));
        assertFalse(strategy.getMetricsMap().containsKey("RELIANCE"));
    }

    @Test
    void testBreakoutRadarThrottlesRepeatedTicks() {
        when(positionSizingEngine.canTradeToday()).thenReturn(true);
        strategy.updateReferenceData("TATACONSUM", 970.0, 981.8, 2_000_000, 2.0);

        // Tick 1 above PDH (982.0 >= 981.8) -> 1 tick, gate not passed yet
        strategy.onTick("TATACONSUM", 982.0, 100_000, 500_000, 200_000, true, java.time.LocalTime.of(10, 0));
        CandidateMetrics m = strategy.getMetricsMap().get("TATACONSUM");
        assertNotNull(m);
        assertEquals(1, m.getConsecutiveBreakoutTicks());

        // Tick 2 above PDH -> 2 ticks, confirms initial breakout
        strategy.onTick("TATACONSUM", 982.0, 101_000, 500_000, 200_000, true, java.time.LocalTime.of(10, 0));
        assertEquals(2, m.getConsecutiveBreakoutTicks());

        // Ticks 3, 4, 5 arriving immediately (rapid subsequent ticks)
        strategy.onTick("TATACONSUM", 982.1, 102_000, 500_000, 200_000, true, java.time.LocalTime.of(10, 0));
        strategy.onTick("TATACONSUM", 982.1, 103_000, 500_000, 200_000, true, java.time.LocalTime.of(10, 0));
        strategy.onTick("TATACONSUM", 982.1, 104_000, 500_000, 200_000, true, java.time.LocalTime.of(10, 0));
        assertEquals(5, m.getConsecutiveBreakoutTicks());

        // Price dips below PDH (980.0 < 981.8) -> reset
        strategy.onTick("TATACONSUM", 980.0, 105_000, 500_000, 200_000, true, java.time.LocalTime.of(10, 1));
        assertEquals(0, m.getConsecutiveBreakoutTicks());
    }
}
