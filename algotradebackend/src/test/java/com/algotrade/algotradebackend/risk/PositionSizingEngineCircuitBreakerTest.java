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
}
