package com.algotrade.algotradebackend.strategy.momentum_breakout;

import com.algotrade.algotradebackend.broker.BrokerGateway;
import com.algotrade.algotradebackend.broker.BrokerGatewayManager;
import com.algotrade.algotradebackend.core.model.ExecutionMode;
import com.algotrade.algotradebackend.core.model.Position;
import com.algotrade.algotradebackend.core.model.TradeOrder;
import com.algotrade.algotradebackend.core.service.EngineActivityFeedService;
import com.algotrade.algotradebackend.core.service.MarketCalendarService;
import com.algotrade.algotradebackend.persistence.entity.PaperActivePositionEntity;
import com.algotrade.algotradebackend.persistence.repository.PaperActivePositionRepository;
import com.algotrade.algotradebackend.risk.PositionSizingEngine;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.springframework.test.util.ReflectionTestUtils;

import java.time.Instant;
import java.time.LocalTime;
import java.time.temporal.ChronoUnit;
import java.util.Optional;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.ArgumentMatchers.*;
import static org.mockito.Mockito.*;

public class StopLadderScenarioTest {

    private MomentumScoringEngine scoringEngine;
    private PositionSizingEngine positionSizingEngine;
    private BrokerGatewayManager brokerGatewayManager;
    private BrokerGateway mockGateway;
    private MarketCalendarService marketCalendarService;
    private EngineActivityFeedService activityFeedService;
    private PaperActivePositionRepository mockPositionRepo;
    private MomentumBreakoutStrategy strategy;

    @BeforeEach
    void setUp() {
        scoringEngine = new MomentumScoringEngine();
        positionSizingEngine = mock(PositionSizingEngine.class);
        brokerGatewayManager = mock(BrokerGatewayManager.class);
        mockGateway = mock(BrokerGateway.class);
        marketCalendarService = mock(MarketCalendarService.class);
        activityFeedService = new EngineActivityFeedService();
        mockPositionRepo = mock(PaperActivePositionRepository.class);

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
        ReflectionTestUtils.setField(strategy, "paperActivePositionRepository", mockPositionRepo);
    }

    private void setupActivePosition(String symbol, int qty, double entryPrice, double initialStop) {
        strategy.updateReferenceData(symbol, entryPrice - 10, entryPrice - 2, 10_000_000, 2.5);
        ReflectionTestUtils.setField(strategy, "activeSymbol", symbol);
        ReflectionTestUtils.setField(strategy, "lifecycleState", TradeLifecycleState.ACTIVE_POSITION);
        ReflectionTestUtils.setField(strategy, "activeSlOrderId", "ORD-SL-1");
        ReflectionTestUtils.setField(strategy, "confirmedEntryPrice", entryPrice);
        ReflectionTestUtils.setField(strategy, "currentProtectedQuantity", qty);
        ReflectionTestUtils.setField(strategy, "initialStructuralStop", initialStop);
        ReflectionTestUtils.setField(strategy, "currentArmedStopPrice", initialStop);
        ReflectionTestUtils.setField(strategy, "unitRisk", entryPrice - initialStop);
        ReflectionTestUtils.setField(strategy, "strategyHighWaterMark", entryPrice);
        ReflectionTestUtils.setField(strategy, "entryFillTimeUtc", Instant.now());
        ReflectionTestUtils.setField(strategy, "lastHwmStepTsUtc", Instant.now());
        ReflectionTestUtils.setField(strategy, "armedPair", StopLadder.stage0Candidate(initialStop, 0.05, 0.0015));
        ReflectionTestUtils.setField(strategy, "logicalStage", 0);

        Position initPos = new Position(symbol, qty, entryPrice, entryPrice, initialStop, initialStop, entryPrice, Instant.now(), ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition()).thenReturn(Optional.of(initPos));

        TradeOrder openSl = new TradeOrder("ORD-SL-1", symbol, "SELL", qty, initialStop - 1.0, initialStop, 0.0, "SL", "TRIGGER PENDING", ExecutionMode.PAPER, Instant.now());
        when(mockGateway.getOrderStatus("ORD-SL-1")).thenReturn(openSl);
    }

    @Test
    @DisplayName("S1: +0.6R reached at minute 1 -> no modify until 3:00, then Stage 1 soft stop")
    void testScenarioS1_IncubationGateAndStage1Trigger() {
        // Entry at 1000.0, stop at 990.0 (1R = 10.0, +0.6R trigger = 1006.0)
        setupActivePosition("TCS", 10, 1000.0, 990.0);
        assertEquals(0, strategy.getLogicalStage());

        // Minute 1: Price touches 1006.50 (+0.65R) inside 3-min incubation window
        Position posMin1 = new Position("TCS", 10, 1000.0, 1006.50, 990.0, 990.0, 1006.50, Instant.now(), ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition()).thenReturn(Optional.of(posMin1));

        strategy.onTick("TCS", 1006.50, 100_000, 1_000_000, 500_000, false, LocalTime.of(9, 31));

        // Peak06 is latched, but incubation is not complete; no SL modification yet
        assertTrue(strategy.isPeak06Latched(), "Peak +0.6R must be latched");
        assertFalse(strategy.isIncubDoneLatched(), "Incubation must NOT be latched at minute 1");
        verify(mockGateway, never()).modifyOrder(eq("ORD-SL-1"), anyDouble(), anyDouble());

        // Fast-forward entryFillTimeUtc back 3 minutes and 5 seconds to simulate incubation expiry
        Instant threeMinsAgo = Instant.now().minus(185, ChronoUnit.SECONDS);
        ReflectionTestUtils.setField(strategy, "entryFillTimeUtc", threeMinsAgo);

        // Next tick at 1005.0 (after 3m) triggers Stage 1 candidate: floor(1000 - 0.35 * 10) = 996.50
        Position posMin3 = new Position("TCS", 10, 1000.0, 1005.0, 990.0, 990.0, 1006.50, threeMinsAgo, ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition()).thenReturn(Optional.of(posMin3));

        strategy.onTick("TCS", 1005.0, 150_000, 1_000_000, 500_000, false, LocalTime.of(9, 34));

        assertTrue(strategy.isIncubDoneLatched(), "Incubation must now be latched");
        assertEquals(1, strategy.getLogicalStage(), "Strategy must advance to Stage 1");
        verify(mockGateway, atLeastOnce()).modifyOrder(eq("ORD-SL-1"), anyDouble(), anyDouble());
    }

    @Test
    @DisplayName("S2: Fast surge to +1.2R inside incubation -> Stage 2 overrides incubation")
    void testScenarioS2_FastSurgeToStage2OverridesIncubation() {
        // Entry 1000.0, S0 990.0 (1R = 10.0, +1.2R trigger = 1012.0)
        setupActivePosition("INFY", 10, 1000.0, 990.0);
        assertEquals(0, strategy.getLogicalStage());

        // Fast surge to 1015.0 (+1.5R) at minute 1:30 (inside incubation)
        Position surgePos = new Position("INFY", 10, 1000.0, 1015.0, 990.0, 990.0, 1015.0, Instant.now(), ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition()).thenReturn(Optional.of(surgePos));

        strategy.onTick("INFY", 1015.0, 200_000, 1_000_000, 500_000, false, LocalTime.of(9, 31));

        // Must lock cost floor immediately without waiting for 3-minute incubation
        assertTrue(strategy.getLogicalStage() >= 2, "Strategy must advance directly to Stage 2 or 3");
        verify(mockGateway, atLeastOnce()).modifyOrder(eq("ORD-SL-1"), anyDouble(), anyDouble());
    }

    @Test
    @DisplayName("S3: Retest to entry after Stage 1 -> stop holds without regression or stop-out")
    void testScenarioS3_RetestToEntryAfterStage1HoldsStop() {
        setupActivePosition("RELIANCE", 10, 1000.0, 990.0);
        ReflectionTestUtils.setField(strategy, "incubDoneLatched", true);
        ReflectionTestUtils.setField(strategy, "peak06Latched", true);
        ReflectionTestUtils.setField(strategy, "logicalStage", 1);
        double stage1Stop = 996.50;
        StopLadder.StopOrderPair armedS1 = new StopLadder.StopOrderPair(stage1Stop, 995.00);
        ReflectionTestUtils.setField(strategy, "armedPair", armedS1);
        ReflectionTestUtils.setField(strategy, "currentArmedStopPrice", stage1Stop);

        // Price retests to 999.0 (below entry, but comfortably above -0.35R stop 996.50)
        Position retestPos = new Position("RELIANCE", 10, 1000.0, 999.0, 990.0, stage1Stop, 1007.0, Instant.now(), ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition()).thenReturn(Optional.of(retestPos));

        strategy.onTick("RELIANCE", 999.0, 300_000, 1_000_000, 500_000, false, LocalTime.of(9, 40));

        assertEquals(TradeLifecycleState.ACTIVE_POSITION, strategy.getLifecycleState());
        assertEquals(1, strategy.getLogicalStage(), "Stage 1 must be maintained");
        assertEquals(stage1Stop, strategy.getCurrentArmedStopPrice(), "Stop price must not regress");
    }

    @Test
    @DisplayName("S5: Stage 2 trigger, gap-down below limit -> resting OPEN stop escalated in-place")
    void testScenarioS5_GapDownOpenStopEscalation() {
        setupActivePosition("TATAMOTORS", 10, 1000.0, 990.0);
        ReflectionTestUtils.setField(strategy, "logicalStage", 2);
        ReflectionTestUtils.setField(strategy, "protectedGraceStartTimeMs", System.currentTimeMillis() - 6000L);

        // Stop order triggered by broker with gap-down -> status is OPEN with statusMessage
        TradeOrder openOrder = new TradeOrder(
            "ORD-SL-1", "TATAMOTORS", "SELL", 10, 992.0, 995.0, 0.0,
            "SL", "OPEN", ExecutionMode.PAPER, Instant.now(), "SL261017001",
            0, 10, "TRIGGERED_UNFILLED_GAP_DOWN"
        );
        when(mockGateway.getOrderStatus("ORD-SL-1")).thenReturn(openOrder);

        Position pos = new Position("TATAMOTORS", 10, 1000.0, 988.0, 990.0, 995.0, 1012.0, Instant.now(), ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition()).thenReturn(Optional.of(pos));
        when(mockGateway.getBestBid("TATAMOTORS")).thenReturn(Optional.of(988.0));
        when(mockGateway.getLowerCircuitLimit("TATAMOTORS")).thenReturn(Optional.of(900.0));

        // When watchdog evaluates the resting OPEN stop, it escalates via modifyOrder without creating a new order
        strategy.onTick("TATAMOTORS", 988.0, 400_000, 1_000_000, 500_000, false, LocalTime.of(9, 50));

        verify(mockGateway, atLeastOnce()).modifyOrder(eq("ORD-SL-1"), eq(10), anyDouble(), eq(0.0));
        // Single-working-sell invariant: no new sell limit order placed
        verify(mockGateway, never()).placeOrder(eq("TATAMOTORS"), eq("SELL"), anyInt(), anyDouble(), eq(0.0), eq("LIMIT"), any());
    }

    @Test
    @DisplayName("S6: Partial fill of triggered stop -> only pending quantity is escalated")
    void testScenarioS6_PartialFillPendingQuantityEscalated() {
        setupActivePosition("HDFCBANK", 10, 1600.0, 1580.0);
        ReflectionTestUtils.setField(strategy, "protectedGraceStartTimeMs", System.currentTimeMillis() - 6000L);

        // 6 shares filled, 4 shares pending
        TradeOrder partiallyFilled = new TradeOrder(
            "ORD-SL-1", "HDFCBANK", "SELL", 10, 1585.0, 1590.0, 1585.0,
            "SL", "OPEN", ExecutionMode.PAPER, Instant.now(), "SL261017002",
            6, 4, "PARTIALLY_FILLED_GAP"
        );
        when(mockGateway.getOrderStatus("ORD-SL-1")).thenReturn(partiallyFilled);

        Position pos = new Position("HDFCBANK", 4, 1600.0, 1570.0, 1580.0, 1590.0, 1620.0, Instant.now(), ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition()).thenReturn(Optional.of(pos));
        when(mockGateway.getBestBid("HDFCBANK")).thenReturn(Optional.of(1570.0));
        when(mockGateway.getLowerCircuitLimit("HDFCBANK")).thenReturn(Optional.of(1440.0));

        strategy.onTick("HDFCBANK", 1570.0, 500_000, 1_000_000, 500_000, false, LocalTime.of(10, 0));

        // Only pending quantity (4 shares) modified
        verify(mockGateway, atLeastOnce()).modifyOrder(eq("ORD-SL-1"), eq(4), anyDouble(), eq(0.0));
    }

    @Test
    @DisplayName("S8: 15:05 square-off with resting OPEN stop -> modifies existing order in-place")
    void testScenarioS8_1505SquareOffModifiesExistingOpenStopInPlace() {
        setupActivePosition("ICICIBANK", 10, 1200.0, 1185.0);

        TradeOrder openOrder = new TradeOrder(
            "ORD-SL-1", "ICICIBANK", "SELL", 10, 1188.0, 1190.0, 0.0,
            "SL", "OPEN", ExecutionMode.PAPER, Instant.now(), "SL261017003",
            0, 10, "TRIGGERED"
        );
        when(mockGateway.getOrderStatus("ORD-SL-1")).thenReturn(openOrder);

        Position pos = new Position("ICICIBANK", 10, 1200.0, 1186.0, 1185.0, 1190.0, 1210.0, Instant.now(), ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition())
            .thenReturn(Optional.of(pos))
            .thenReturn(Optional.empty()); // Flat after fill
        when(mockGateway.getBestBid("ICICIBANK")).thenReturn(Optional.of(1186.0));
        when(mockGateway.getLowerCircuitLimit("ICICIBANK")).thenReturn(Optional.of(1080.0));

        strategy.executeSquareOff();

        // In-place modification of ORD-SL-1 to Step 1 emergency limit
        verify(mockGateway, atLeastOnce()).modifyOrder(eq("ORD-SL-1"), eq(10), anyDouble(), eq(0.0));
        // Single-working-sell invariant: never place duplicate SELL order
        verify(mockGateway, never()).placeOrder(eq("ICICIBANK"), eq("SELL"), anyInt(), anyDouble(), eq(0.0), eq("LIMIT"), any());
        assertEquals(TradeLifecycleState.FLAT, strategy.getLifecycleState());
    }

    @Test
    @DisplayName("S10: 25-min stagnation branch (b) at bid >= floor -> PROTECTED_EXIT")
    void testScenarioS10_25MinStagnationBranchB_ProtectedExit() {
        setupActivePosition("WIPRO", 100, 500.0, 495.0);
        ReflectionTestUtils.setField(strategy, "logicalStage", 1);
        StopLadder.StopOrderPair armed = new StopLadder.StopOrderPair(496.75, 496.0);
        ReflectionTestUtils.setField(strategy, "armedPair", armed);
        ReflectionTestUtils.setField(strategy, "currentArmedStopPrice", 496.75);

        // Elapsed time: 26 minutes with no HWM progress in last 16 minutes
        Instant t0 = Instant.now().minus(26, ChronoUnit.MINUTES);
        Instant hwmStep = Instant.now().minus(16, ChronoUnit.MINUTES);
        ReflectionTestUtils.setField(strategy, "entryFillTimeUtc", t0);
        ReflectionTestUtils.setField(strategy, "lastHwmStepTsUtc", hwmStep);
        ReflectionTestUtils.setField(strategy, "hwm15MinAgo", 504.0);
        ReflectionTestUtils.setField(strategy, "strategyHighWaterMark", 504.0);

        Position pos = new Position("WIPRO", 100, 500.0, 501.50, 495.0, 496.75, 504.0, t0, ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition()).thenReturn(Optional.of(pos));
        when(mockGateway.getBestBid("WIPRO")).thenReturn(Optional.of(501.50)); // bid >= floor ~500.80
        when(mockGateway.getLowerCircuitLimit("WIPRO")).thenReturn(Optional.of(450.0));

        TradeOrder openSl = new TradeOrder("ORD-SL-1", "WIPRO", "SELL", 100, 496.0, 496.75, 0.0, "SL", "TRIGGER PENDING", ExecutionMode.PAPER, Instant.now());
        when(mockGateway.getOrderStatus("ORD-SL-1")).thenReturn(openSl);

        TradeOrder exitOrder = new TradeOrder("ORD-EX-1", "WIPRO", "SELL", 100, 501.50, 0.0, 0.0, "LIMIT", "OPEN", ExecutionMode.PAPER, Instant.now());
        when(mockGateway.placeOrder(eq("WIPRO"), eq("SELL"), anyInt(), anyDouble(), anyDouble(), eq("LIMIT"), any())).thenReturn(exitOrder);

        strategy.onTick("WIPRO", 501.50, 100_000, 1_000_000, 500_000, false, LocalTime.of(9, 56));

        assertTrue(strategy.isStagDoneLatched(), "Stagnation latch must be set");
        assertEquals("PROTECTED", strategy.getActiveExitPolicy(), "Exit policy must be PROTECTED");
    }

    @Test
    @DisplayName("S12: 45-min supervisor -> Stage 1 mandates close, Stage 3 holds runner")
    void testScenarioS12_45MinSupervisorStage1VsStage3() {
        // Case A: Stage 1 at 46 minutes -> Mandatory close
        setupActivePosition("SBIN", 10, 800.0, 792.0);
        ReflectionTestUtils.setField(strategy, "logicalStage", 1);
        Instant t46MinsAgo = Instant.now().minus(46, ChronoUnit.MINUTES);
        ReflectionTestUtils.setField(strategy, "entryFillTimeUtc", t46MinsAgo);

        Position posStage1 = new Position("SBIN", 10, 800.0, 801.0, 792.0, 797.20, 805.0, t46MinsAgo, ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition()).thenReturn(Optional.of(posStage1));
        when(mockGateway.getBestBid("SBIN")).thenReturn(Optional.of(801.0));
        when(mockGateway.getLowerCircuitLimit("SBIN")).thenReturn(Optional.of(720.0));

        strategy.onTick("SBIN", 801.0, 100_000, 1_000_000, 500_000, false, LocalTime.of(10, 16));

        assertTrue(strategy.isT45DoneLatched(), "45m supervisor latch must be set");
        assertEquals(TradeLifecycleState.PENDING_EXIT, strategy.getLifecycleState(), "Stage 1 must transition to PENDING_EXIT");

        // Case B: Stage 3 at 46 minutes -> Holds runner!
        MomentumBreakoutStrategy strategyRunner = new MomentumBreakoutStrategy(
            scoringEngine, positionSizingEngine, brokerGatewayManager, marketCalendarService, activityFeedService
        );
        ReflectionTestUtils.setField(strategyRunner, "activeSymbol", "SBIN");
        ReflectionTestUtils.setField(strategyRunner, "lifecycleState", TradeLifecycleState.ACTIVE_POSITION);
        ReflectionTestUtils.setField(strategyRunner, "activeSlOrderId", "ORD-SL-2");
        ReflectionTestUtils.setField(strategyRunner, "logicalStage", 3);
        ReflectionTestUtils.setField(strategyRunner, "entryFillTimeUtc", t46MinsAgo);
        ReflectionTestUtils.setField(strategyRunner, "strategyHighWaterMark", 820.0);
        ReflectionTestUtils.setField(strategyRunner, "confirmedEntryPrice", 800.0);
        ReflectionTestUtils.setField(strategyRunner, "unitRisk", 8.0);
        ReflectionTestUtils.setField(strategyRunner, "armedPair", new StopLadder.StopOrderPair(810.0, 808.0));

        Position posStage3 = new Position("SBIN", 10, 800.0, 815.0, 792.0, 810.0, 820.0, t46MinsAgo, ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition()).thenReturn(Optional.of(posStage3));

        strategyRunner.onTick("SBIN", 815.0, 100_000, 1_000_000, 500_000, false, LocalTime.of(10, 16));

        assertTrue(strategyRunner.isT45DoneLatched(), "45m supervisor latch must be set");
        assertEquals(TradeLifecycleState.ACTIVE_POSITION, strategyRunner.getLifecycleState(), "Stage 3 runner must stay in ACTIVE_POSITION");
    }

    @Test
    @DisplayName("C11 Recovery Matrix: Broker active stop rehydrates state without re-placing order")
    void testRecoveryMatrix_BrokerActiveStopRehydratesCleanly() {
        PaperActivePositionEntity entity = new PaperActivePositionEntity();
        entity.setSymbol("KOTAKBANK");
        entity.setQuantity(10);
        entity.setEntryPrice(1800.0);
        entity.setCurrentPrice(1815.0);
        entity.setInitialStopLoss(1785.0);
        entity.setActiveTrailingStop(1795.0);
        entity.setHighWaterMark(1820.0);
        entity.setLogicalStage(1);
        entity.setArmedLimit(1792.0);
        entity.setActiveSlOrderId("ORD-SL-KOTAK");
        entity.setMode("PAPER");

        when(mockPositionRepo.findFirstByOrderByEntryTimeDesc()).thenReturn(Optional.of(entity));

        Position brokerPos = new Position("KOTAKBANK", 10, 1800.0, 1815.0, 1785.0, 1795.0, 1820.0, Instant.now(), ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition()).thenReturn(Optional.of(brokerPos));

        TradeOrder brokerStop = new TradeOrder("ORD-SL-KOTAK", "KOTAKBANK", "SELL", 10, 1792.0, 1795.0, 0.0, "SL", "TRIGGER PENDING", ExecutionMode.PAPER, Instant.now());
        when(mockGateway.getActiveStopOrder(eq("KOTAKBANK"), any())).thenReturn(Optional.of(brokerStop));
        when(mockGateway.getOrderStatus("ORD-SL-KOTAK")).thenReturn(brokerStop);

        strategy.rehydrateActivePositionIfPresent();

        assertEquals(TradeLifecycleState.ACTIVE_POSITION, strategy.getLifecycleState());
        assertEquals("KOTAKBANK", strategy.getActiveSymbol());
        assertEquals("ORD-SL-KOTAK", strategy.getActiveSlOrderId());
        assertEquals(1, strategy.getLogicalStage());
        assertEquals(1795.0, strategy.getCurrentArmedStopPrice());

        // Zero duplicate orders placed
        verify(mockGateway, never()).placeOrder(anyString(), anyString(), anyInt(), anyDouble(), anyDouble(), anyString(), anyString());
    }

    @Test
    @DisplayName("Manual SL: In-place broker modification + automated ladder continues seamlessly")
    void testManualStopLossUpdate_ModifiesInPlaceAndContinuesAutomatedLadder() {
        setupActivePosition("TCS", 10, 1000.0, 990.0);
        Position activePos = new Position("TCS", 10, 1000.0, 1010.0, 990.0, 990.0, 1010.0, Instant.now(), ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition()).thenReturn(Optional.of(activePos));

        // 1. User sets manual stop to 1005.0 (below LTP 1010.0, above S0 990.0)
        var res = strategy.updateManualStopLoss("TCS", 1005.0);
        assertTrue((Boolean) res.get("success"));
        assertEquals(1005.0, (Double) res.get("triggerPrice"));
        assertEquals(1005.0, strategy.getCurrentArmedStopPrice());

        // In-place modification of ORD-SL-1 with zero duplicate sell orders placed
        verify(mockGateway, atLeastOnce()).modifyOrder(eq("ORD-SL-1"), eq(10), anyDouble(), eq(1005.0));
        verify(mockGateway, never()).placeOrder(anyString(), eq("SELL"), anyInt(), anyDouble(), anyDouble(), anyString(), anyString());

        // 2. Price pulls back to 1006.0: Stop holds at 1005.0 without regression
        Position pullbackPos = new Position("TCS", 10, 1000.0, 1006.0, 990.0, 1005.0, 1010.0, Instant.now(), ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition()).thenReturn(Optional.of(pullbackPos));
        strategy.onTick("TCS", 1006.0, 100_000, 1_000_000, 500_000, false, LocalTime.of(9, 45));
        assertEquals(1005.0, strategy.getCurrentArmedStopPrice(), "Stop must not regress on pullback");

        // 3. Price rallies to 1025.0 (+2.5R, Stage 3 runner): Automated ladder smoothly takes over and ratchets higher
        // Fast-forward lastStopModifyTimeMs past 10s throttle window
        ReflectionTestUtils.setField(strategy, "lastStopModifyTimeMs", System.currentTimeMillis() - 15000L);
        Position rallyPos = new Position("TCS", 10, 1000.0, 1025.0, 990.0, 1005.0, 1025.0, Instant.now(), ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition()).thenReturn(Optional.of(rallyPos));
        strategy.onTick("TCS", 1025.0, 200_000, 1_000_000, 500_000, false, LocalTime.of(9, 50));

        assertTrue(strategy.getLogicalStage() >= 2, "Automated ladder must advance");
        assertTrue(strategy.getCurrentArmedStopPrice() > 1005.0, "Automated ladder must ratchet stop higher than manual stop once trail candidate exceeds it");
    }

    @Test
    @DisplayName("Manual SL: Boundary guards reject price inversion, circuit, and S0 violations")
    void testManualStopLossUpdate_RejectsBoundaryViolations() {
        setupActivePosition("INFY", 10, 1000.0, 990.0);
        Position pos = new Position("INFY", 10, 1000.0, 1010.0, 990.0, 990.0, 1010.0, Instant.now(), ExecutionMode.PAPER);
        when(mockGateway.getOpenPosition()).thenReturn(Optional.of(pos));
        when(mockGateway.getLowerCircuitLimit("INFY")).thenReturn(Optional.of(900.0));

        // Inversion violation: stop >= LTP (1015.0 >= 1010.0)
        assertThrows(IllegalArgumentException.class, () -> strategy.updateManualStopLoss("INFY", 1015.0));

        // Circuit violation: stop <= Lower Circuit (850.0 <= 900.0)
        assertThrows(IllegalArgumentException.class, () -> strategy.updateManualStopLoss("INFY", 850.0));

        // Risk expansion violation: stop < initial structural stop S0 (980.0 < 990.0)
        assertThrows(IllegalArgumentException.class, () -> strategy.updateManualStopLoss("INFY", 980.0));

        // Symbol mismatch: "TCS" vs active "INFY"
        assertThrows(IllegalArgumentException.class, () -> strategy.updateManualStopLoss("TCS", 1005.0));
    }
}

