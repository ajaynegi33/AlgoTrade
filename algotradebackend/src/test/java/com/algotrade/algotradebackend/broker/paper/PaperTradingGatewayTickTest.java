package com.algotrade.algotradebackend.broker.paper;

import com.algotrade.algotradebackend.broker.BrokerGatewayManager;
import com.algotrade.algotradebackend.core.model.Position;
import com.algotrade.algotradebackend.persistence.entity.PaperTradeEntity;
import com.algotrade.algotradebackend.persistence.repository.PaperTradeRepository;
import com.algotrade.algotradebackend.strategy.StrategyManager;
import com.algotrade.algotradebackend.strategy.TradingStrategy;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.mockito.ArgumentCaptor;

import java.util.List;
import java.util.Optional;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.Mockito.*;

public class PaperTradingGatewayTickTest {

    private PaperTradeRepository paperTradeRepository;
    private PaperTradingGateway paperGateway;
    private BrokerGatewayManager brokerGatewayManager;
    private StrategyManager strategyManager;
    private TradingStrategy mockStrategy;

    @BeforeEach
    void setUp() {
        paperTradeRepository = mock(PaperTradeRepository.class);
        paperGateway = new PaperTradingGateway(paperTradeRepository, 10000.0);
        brokerGatewayManager = mock(BrokerGatewayManager.class);
        mockStrategy = mock(TradingStrategy.class);

        when(mockStrategy.getStrategyId()).thenReturn("MOMENTUM_BREAKOUT");
        when(mockStrategy.getStrategyName()).thenReturn("Momentum Breakout");
        when(brokerGatewayManager.getPaperTradingGateway()).thenReturn(paperGateway);

        strategyManager = new StrategyManager(List.of(mockStrategy), brokerGatewayManager);
    }

    @Test
    void testStrategyManagerTickDispatchesToPaperGatewayAndTriggersStopLoss() {
        // 1. Place Paper Buy Order (simulates entry fill)
        paperGateway.placeOrder("RELIANCE", "BUY", 10, 1000.0, 0.0, "LIMIT");
        Optional<Position> posOpt = paperGateway.getOpenPosition();
        assertTrue(posOpt.isPresent());
        assertEquals("RELIANCE", posOpt.get().symbol());
        assertEquals(10, posOpt.get().quantity());

        // 2. Place Paper Stop-Loss Order at 990.0 trigger
        paperGateway.placeOrder("RELIANCE", "SELL", 10, 988.0, 990.0, "SL");
        assertEquals(990.0, paperGateway.getOpenPosition().get().activeTrailingStop());

        // 3. Favorable tick arrives: LTP rises to 1010.0
        strategyManager.onTick("RELIANCE", 1010.0, 500_000, 1_000_000, 500_000);
        verify(mockStrategy).onTick("RELIANCE", 1010.0, 500_000, 1_000_000, 500_000);
        assertEquals(1010.0, paperGateway.getOpenPosition().get().highWaterMark());
        assertTrue(paperGateway.getOpenPosition().isPresent(), "Position should still be open above SL");

        // 4. Adverse tick arrives: LTP drops to 989.5 (breaching SL 990.0)
        strategyManager.onTick("RELIANCE", 989.5, 600_000, 500_000, 1_000_000);

        // 5. Position must be closed immediately
        assertTrue(paperGateway.getOpenPosition().isEmpty(), "Position must close when SL is breached");

        // 6. Verify trade persisted with STOP_LOSS_HIT
        ArgumentCaptor<PaperTradeEntity> captor = ArgumentCaptor.forClass(PaperTradeEntity.class);
        verify(paperTradeRepository).save(captor.capture());
        PaperTradeEntity saved = captor.getValue();
        assertEquals("RELIANCE", saved.getSymbol());
        assertEquals(10, saved.getQuantity());
        assertEquals("STOP_LOSS_HIT", saved.getExitReason());
        assertTrue(saved.getGrossPnL() < 0, "Trade should record loss");
        assertTrue(saved.getTotalCharges() > 0, "Taxes & charges should be deducted");
    }
}
