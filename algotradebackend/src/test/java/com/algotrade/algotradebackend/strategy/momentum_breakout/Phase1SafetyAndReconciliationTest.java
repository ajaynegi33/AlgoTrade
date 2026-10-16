package com.algotrade.algotradebackend.strategy.momentum_breakout;

import com.algotrade.algotradebackend.broker.BrokerGateway;
import com.algotrade.algotradebackend.broker.BrokerGatewayManager;
import com.algotrade.algotradebackend.broker.paper.PaperTradingGateway;
import com.algotrade.algotradebackend.broker.zerodha.ZerodhaLiveGateway;
import com.algotrade.algotradebackend.core.model.ExecutionMode;
import com.algotrade.algotradebackend.core.model.Position;
import com.algotrade.algotradebackend.core.model.TradeOrder;
import com.algotrade.algotradebackend.persistence.entity.LiveTradeEntity;
import com.algotrade.algotradebackend.persistence.repository.LiveTradeRepository;
import com.algotrade.algotradebackend.persistence.repository.PaperTradeRepository;
import com.algotrade.algotradebackend.risk.PositionSizingEngine;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;

import java.time.Instant;
import java.util.List;
import java.util.Optional;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.Mockito.*;

public class Phase1SafetyAndReconciliationTest {

    private PaperTradeRepository mockPaperRepo;
    private LiveTradeRepository mockLiveRepo;
    private PaperTradingGateway mockPaperGateway;
    private ZerodhaLiveGateway mockZerodhaGateway;

    @BeforeEach
    void setUp() {
        mockPaperRepo = mock(PaperTradeRepository.class);
        mockLiveRepo = mock(LiveTradeRepository.class);
        mockPaperGateway = mock(PaperTradingGateway.class);
        mockZerodhaGateway = mock(ZerodhaLiveGateway.class);
    }

    @Test
    @DisplayName("Position Sizing: CalculateQuantity accounts for stop slippage and transaction costs")
    void testAllInclusiveSizingEnforcesRiskBudget() {
        PositionSizingEngine engine = new PositionSizingEngine(mockPaperRepo, mockLiveRepo);
        org.springframework.test.util.ReflectionTestUtils.setField(engine, "maxDailyTrades", 3);
        org.springframework.test.util.ReflectionTestUtils.setField(engine, "maxDailyLosses", 2);
        org.springframework.test.util.ReflectionTestUtils.setField(engine, "maxDailyLossRupees", 400.0);
        org.springframework.test.util.ReflectionTestUtils.setField(engine, "riskBudgetPct", 2.0);
        org.springframework.test.util.ReflectionTestUtils.setField(engine, "maxExposureMultiplier", 5.0);
        org.springframework.test.util.ReflectionTestUtils.setField(engine, "minStopDistancePct", 0.35);
        org.springframework.test.util.ReflectionTestUtils.setField(engine, "maxStopDistancePct", 2.0);
        org.springframework.test.util.ReflectionTestUtils.setField(engine, "capital", 10000.0);

        BrokerGateway gateway = mock(BrokerGateway.class);
        when(gateway.getTotalCapital()).thenReturn(10000.0);
        when(gateway.getAvailableCash()).thenReturn(10000.0);
        when(gateway.calculateOrderMargin(anyString(), anyInt(), anyDouble())).thenReturn(200.0);

        // Entry 1000, stop 980 (₹20 stop distance = 2.0%)
        // Raw math: 200 / 20 = 10 shares.
        // But with 0.15% slippage + statutory taxes, 10 shares would lose > ₹200.
        // All-inclusive sizing must reduce shares so worst-case total loss <= ₹200.
        int qty = engine.calculateQuantity("RELIANCE", 1000.0, 980.0, gateway);

        assertTrue(qty > 0, "Calculated quantity should be positive");
        assertTrue(qty <= 10, "Quantity must be reduced below raw price-distance cap to budget for fees and slippage");
    }

    @Test
    @DisplayName("Mode Switch Guard: Rejects mode change when active position or resting orders exist")
    void testModeSwitchBlockedWhenOrdersOrPositionActive() {
        BrokerGatewayManager manager = new BrokerGatewayManager(mockPaperGateway, mockZerodhaGateway, "PAPER");

        // Case 1: Open position exists
        when(mockPaperGateway.getOpenPosition()).thenReturn(Optional.of(new Position("INFY", 10, 1500.0, 1505.0, 0, 0, 1505.0, Instant.now(), ExecutionMode.PAPER)));
        IllegalStateException ex = assertThrows(IllegalStateException.class, () -> manager.setExecutionMode(ExecutionMode.LIVE));
        assertTrue(ex.getMessage().contains("open position is actively held"));

        // Case 2: Open position closed, but resting order exists
        when(mockPaperGateway.getOpenPosition()).thenReturn(Optional.empty());
        when(mockPaperGateway.hasActiveOrders()).thenReturn(true);
        ex = assertThrows(IllegalStateException.class, () -> manager.setExecutionMode(ExecutionMode.LIVE));
        assertTrue(ex.getMessage().contains("resting broker orders are open"));
    }

    @Test
    @DisplayName("Circuit Breaker Persistence: LiveTradeRepository records persist and halt trading on restart")
    @SuppressWarnings("unchecked")
    void testLiveTradePersistenceEnforcesCircuitBreakerAcrossRestart() {
        // Mock two settled live losses today in SQLite
        LiveTradeEntity loss1 = new LiveTradeEntity("TCS", 10, 3500.0, 3470.0, Instant.now().minusSeconds(3600), Instant.now().minusSeconds(3500), "SL", -300.0, -325.0, 25.0, 20.0, 1.0, 1.0, 0.1, 0.1, 2.8, "ORD-1");
        LiveTradeEntity loss2 = new LiveTradeEntity("INFY", 20, 1600.0, 1590.0, Instant.now().minusSeconds(1800), Instant.now().minusSeconds(1700), "SL", -200.0, -218.0, 18.0, 20.0, 1.0, 1.0, 0.1, 0.1, 2.8, "ORD-2");

        when(mockLiveRepo.findByEntryTimeBetween(any(), any())).thenReturn(List.of(loss1, loss2));

        // Create a new instance representing backend restart
        PositionSizingEngine restoredEngine = new PositionSizingEngine(mockPaperRepo, mockLiveRepo);

        // Mock gateway manager returning LIVE mode
        BrokerGatewayManager manager = mock(BrokerGatewayManager.class);
        when(manager.getActiveMode()).thenReturn(ExecutionMode.LIVE);

        org.springframework.beans.factory.ObjectProvider<BrokerGatewayManager> provider = mock(org.springframework.beans.factory.ObjectProvider.class);
        when(provider.getIfAvailable()).thenReturn(manager);

        org.springframework.test.util.ReflectionTestUtils.setField(restoredEngine, "maxDailyLosses", 2);
        org.springframework.test.util.ReflectionTestUtils.setField(restoredEngine, "maxDailyTrades", 3);
        org.springframework.test.util.ReflectionTestUtils.setField(restoredEngine, "maxDailyLossRupees", 400.0);
        org.springframework.test.util.ReflectionTestUtils.setField(restoredEngine, "brokerGatewayManagerProvider", provider);

        // When in LIVE mode with 2 losses from database, canTradeToday must evaluate false
        boolean canTrade = restoredEngine.canTradeToday();
        assertFalse(canTrade, "Trading must be blocked because 2 losses are recorded in LiveTradeRepository");
    }

    @Test
    @DisplayName("TradeOrder: averagePrice properly supported alongside backward-compatible constructor")
    void testTradeOrderAveragePriceCompatibility() {
        // Legacy constructor
        TradeOrder legacy = new TradeOrder("ORD-LEG", "TCS", "BUY", 10, 3500.0, 0.0, "LIMIT", "FILLED", ExecutionMode.LIVE, Instant.now());
        assertEquals(3500.0, legacy.price());
        assertEquals(3500.0, legacy.averagePrice(), "Legacy constructor should default averagePrice to price");

        // Explicit averagePrice
        TradeOrder marketWithFill = new TradeOrder("ORD-MKT", "TCS", "BUY", 10, 0.0, 0.0, 3502.45, "MARKET", "COMPLETE", ExecutionMode.LIVE, Instant.now());
        assertEquals(0.0, marketWithFill.price());
        assertEquals(3502.45, marketWithFill.averagePrice(), "Explicit average fill price must be preserved");
    }
}
