package com.algotrade.algotradebackend.risk;

import com.algotrade.algotradebackend.broker.paper.PaperTradingGateway;
import com.algotrade.algotradebackend.core.model.ExecutionMode;
import com.algotrade.algotradebackend.core.model.Position;
import com.algotrade.algotradebackend.persistence.entity.PaperActivePositionEntity;
import com.algotrade.algotradebackend.persistence.entity.PaperTradeEntity;
import com.algotrade.algotradebackend.persistence.repository.PaperActivePositionRepository;
import com.algotrade.algotradebackend.persistence.repository.PaperTradeRepository;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.mockito.ArgumentCaptor;
import org.springframework.test.util.ReflectionTestUtils;

import java.time.Instant;
import java.util.Collections;
import java.util.List;
import java.util.Optional;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.*;

public class PositionSizingEngineCircuitBreakerTest {

    private PaperTradeRepository paperTradeRepository;
    private PaperActivePositionRepository paperActivePositionRepository;
    private PositionSizingEngine sizingEngine;

    @BeforeEach
    void setUp() {
        paperTradeRepository = mock(PaperTradeRepository.class);
        paperActivePositionRepository = mock(PaperActivePositionRepository.class);
        sizingEngine = new PositionSizingEngine(paperTradeRepository);

        ReflectionTestUtils.setField(sizingEngine, "maxDailyTrades", 3);
        ReflectionTestUtils.setField(sizingEngine, "maxDailyLosses", 2);
    }

    @Test
    void testCanTradeTodayCachingPreventsExcessiveDbCalls() {
        when(paperTradeRepository.findByEntryTimeBetween(any(), any())).thenReturn(Collections.emptyList());

        // Call canTradeToday() 100 times in rapid succession (simulating 100 ticks in 1 second)
        for (int i = 0; i < 100; i++) {
            boolean canTrade = sizingEngine.canTradeToday();
            assertTrue(canTrade);
        }

        // Verify the database repository was only queried ONCE because of the in-memory cache TTL!
        verify(paperTradeRepository, times(1)).findByEntryTimeBetween(any(), any());
    }

    @Test
    void testPaperTradesDoNotIncrementLiveTradesCounter() {
        when(paperTradeRepository.findByEntryTimeBetween(any(), any())).thenReturn(Collections.emptyList());

        sizingEngine.recordTradeDispatched(ExecutionMode.PAPER);
        sizingEngine.recordTradeDispatched(ExecutionMode.PAPER);

        // canTradeToday should still be true because liveTrades is 0 and paperTrades in DB is 0
        assertTrue(sizingEngine.canTradeToday());
    }

    @Test
    void testResetCircuitBreakerClearsLockout() {
        PaperTradeEntity loss1 = mock(PaperTradeEntity.class);
        when(loss1.getNetPnL()).thenReturn(-100.0);
        PaperTradeEntity loss2 = mock(PaperTradeEntity.class);
        when(loss2.getNetPnL()).thenReturn(-150.0);

        when(paperTradeRepository.findByEntryTimeBetween(any(), any())).thenReturn(List.of(loss1, loss2));

        // Circuit breaker should trip
        assertFalse(sizingEngine.canTradeToday());

        // Reset the circuit breaker
        when(paperTradeRepository.findByEntryTimeBetween(any(), any())).thenReturn(Collections.emptyList());
        sizingEngine.resetCircuitBreaker();

        // Should now be permitted to trade again
        assertTrue(sizingEngine.canTradeToday());
    }

    @Test
    void testPaperActivePositionRestoresAcrossRestart() {
        PaperActivePositionEntity savedEntity = new PaperActivePositionEntity(
            "RELIANCE", 15, 2500.0, 2520.0, 2480.0, 2490.0, 2525.0, Instant.now(), "SL-ORD-123"
        );
        when(paperActivePositionRepository.findFirstByOrderByEntryTimeDesc()).thenReturn(Optional.of(savedEntity));

        PaperTradingGateway gateway = new PaperTradingGateway(paperTradeRepository, paperActivePositionRepository, 10000.0);

        Optional<Position> optPos = gateway.getOpenPosition();
        assertTrue(optPos.isPresent());
        assertEquals("RELIANCE", optPos.get().symbol());
        assertEquals(15, optPos.get().quantity());
        assertEquals(2500.0, optPos.get().entryPrice());
        assertEquals(2490.0, optPos.get().activeTrailingStop());
        assertEquals(2525.0, optPos.get().highWaterMark());
    }

    @Test
    void testNoActivePositionWhenRepositoryEmpty() {
        when(paperActivePositionRepository.findFirstByOrderByEntryTimeDesc()).thenReturn(Optional.empty());

        PaperTradingGateway gateway = new PaperTradingGateway(paperTradeRepository, paperActivePositionRepository, 10000.0);

        Optional<Position> optPos = gateway.getOpenPosition();
        assertTrue(optPos.isEmpty());
    }

    @Test
    void testRupeeDrawdownCircuitBreakerTripsInPaperMode() {
        PaperTradeEntity loss = mock(PaperTradeEntity.class);
        when(loss.getNetPnL()).thenReturn(-420.0);
        when(paperTradeRepository.findByEntryTimeBetween(any(), any())).thenReturn(List.of(loss));
        ReflectionTestUtils.setField(sizingEngine, "maxDailyLossRupees", 400.0);

        // Even though loss count is only 1 (< maxDailyLosses = 2), rupee limit has been exceeded!
        assertFalse(sizingEngine.canTradeToday(), "Paper rupee drawdown exceeding ₹400 must halt trading");
    }

    @Test
    void testRupeeDrawdownCircuitBreakerTripsInLiveMode() {
        com.algotrade.algotradebackend.broker.BrokerGatewayManager manager = mock(com.algotrade.algotradebackend.broker.BrokerGatewayManager.class);
        when(manager.getActiveMode()).thenReturn(ExecutionMode.LIVE);
        @SuppressWarnings("unchecked")
        org.springframework.beans.factory.ObjectProvider<com.algotrade.algotradebackend.broker.BrokerGatewayManager> provider = mock(org.springframework.beans.factory.ObjectProvider.class);
        when(provider.getIfAvailable()).thenReturn(manager);

        ReflectionTestUtils.setField(sizingEngine, "brokerGatewayManagerProvider", provider);
        ReflectionTestUtils.setField(sizingEngine, "maxDailyLossRupees", 400.0);

        // Record a single large loss of ₹420 in live mode
        sizingEngine.recordTradeLoss(ExecutionMode.LIVE, 420.0);

        // Even though loss count is only 1 (< maxDailyLosses = 2), rupee limit has been exceeded!
        assertFalse(sizingEngine.canTradeToday(), "Live rupee drawdown exceeding ₹400 must halt trading");
    }

    @Test
    void testDynamicExchangeTickSize() {
        assertEquals(0.01, com.algotrade.algotradebackend.core.util.TickMath.getExchangeTickSize(50.0));
        assertEquals(0.01, com.algotrade.algotradebackend.core.util.TickMath.getExchangeTickSize(249.95));
        assertEquals(0.01, com.algotrade.algotradebackend.core.util.TickMath.getExchangeTickSize(250.00));
        assertEquals(0.05, com.algotrade.algotradebackend.core.util.TickMath.getExchangeTickSize(250.05));
        assertEquals(0.05, com.algotrade.algotradebackend.core.util.TickMath.getExchangeTickSize(1500.00));
    }

    @Test
    void testAllInclusiveSizingGuaranteesRealizedLossWithinBudget() {
        double capital = 10000.0;
        double entryPrice = 1000.0;
        double stopPrice = 985.0; // ₹15 stop distance
        double riskBudgetPct = 2.0; // ₹200 budget
        double slippagePct = 0.15;

        int allInclusiveQty = PositionSizingEngine.calculateAllInclusiveSizingQuantity(
            capital, entryPrice, stopPrice, riskBudgetPct, 5.0, 0.4, 2.0, slippagePct
        );
        assertTrue(allInclusiveQty > 0);

        // Verify total loss (price drop + slippage + statutory taxes & fees) strictly <= ₹200
        double exitPriceWithSlippage = stopPrice * (1.0 - slippagePct / 100.0);
        var charges = com.algotrade.algotradebackend.core.model.ChargesBreakdown.calculate(
            allInclusiveQty, entryPrice, exitPriceWithSlippage
        );
        double totalRealizedLoss = Math.abs(charges.netPnL());
        assertTrue(totalRealizedLoss <= 200.0,
            "Total realized loss (" + totalRealizedLoss + ") must strictly be within ₹200 risk budget");
    }
}
