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
import java.util.Map;
import java.util.Optional;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.ArgumentMatchers.*;
import static org.mockito.Mockito.*;

public class ManualExitTest {

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
    void testExecuteManualExitWhenPositionIsOpenLiquidatesImmediately() {
        // Position is currently active on BHARTIARTL
        Position activePos = new Position(
            "BHARTIARTL",
            21,
            1852.73,
            1866.50,
            1841.40,
            1841.40,
            1868.20,
            Instant.now(),
            ExecutionMode.PAPER
        );
        when(mockGateway.getOpenPosition()).thenReturn(Optional.of(activePos));
        when(mockGateway.placeOrder(eq("BHARTIARTL"), eq("SELL"), eq(21), eq(0.0), eq(0.0), eq("MARKET")))
            .thenReturn(new TradeOrder("ORD-EXIT-1", "BHARTIARTL", "SELL", 21, 1866.50, 0.0, "MARKET", "FILLED", ExecutionMode.PAPER, Instant.now()));

        Map<String, Object> result = strategy.executeManualExit("BHARTIARTL", "MANUAL_OPERATOR_SELL");

        assertNotNull(result);
        assertEquals(true, result.get("success"));
        assertEquals("BHARTIARTL", result.get("symbol"));
        assertEquals(21, result.get("quantity"));
        assertEquals(1852.73, result.get("entryPrice"));
        assertEquals(1866.50, result.get("exitPrice"));
        assertEquals(TradeLifecycleState.FLAT, strategy.getLifecycleState(), "Strategy lifecycle state must be FLAT after manual exit");

        // Verify resting market exit order was placed immediately
        verify(mockGateway, times(1)).placeOrder(eq("BHARTIARTL"), eq("SELL"), eq(21), eq(0.0), eq(0.0), eq("MARKET"));
    }

    @Test
    void testExecuteManualExitWhenAlreadyFlatReturnsGracefully() {
        when(mockGateway.getOpenPosition()).thenReturn(Optional.empty());

        Map<String, Object> result = strategy.executeManualExit(null, "MANUAL_OPERATOR_SELL");

        assertNotNull(result);
        assertEquals(true, result.get("success"));
        assertEquals("FLAT", result.get("status"));
        assertEquals(TradeLifecycleState.FLAT, strategy.getLifecycleState());
        verify(mockGateway, never()).placeOrder(anyString(), anyString(), anyInt(), anyDouble(), anyDouble(), anyString());
    }
}
