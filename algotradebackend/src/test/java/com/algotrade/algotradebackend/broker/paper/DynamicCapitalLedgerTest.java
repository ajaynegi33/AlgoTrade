package com.algotrade.algotradebackend.broker.paper;

import com.algotrade.algotradebackend.persistence.entity.PaperTradeEntity;
import com.algotrade.algotradebackend.persistence.repository.PaperTradeRepository;
import org.junit.jupiter.api.Test;

import java.time.Instant;
import java.util.List;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.when;

public class DynamicCapitalLedgerTest {

    @Test
    void testInitialCapitalRestorationFromPersistedPaperLoss() {
        PaperTradeRepository repository = mock(PaperTradeRepository.class);

        // Simulate existing historical trade with -71.78 net loss
        PaperTradeEntity irfcLossTrade = new PaperTradeEntity(
            "IRFC",
            250,
            124.50,
            124.25,
            Instant.now().minusSeconds(3600),
            Instant.now(),
            "STOP_LOSS_HIT",
            -18.75,
            -71.78,
            53.03,
            40.0,
            2.5,
            1.2,
            0.1,
            2.0,
            7.23
        );
        when(repository.findAll()).thenReturn(List.of(irfcLossTrade));

        // Initialize PaperTradingGateway with 10,000 base capital
        PaperTradingGateway gateway = new PaperTradingGateway(repository, 10000.0);

        assertEquals(10000.0, gateway.getBaseCapital(), 0.001);
        assertEquals(9928.22, gateway.getTotalCapital(), 0.001);
        assertEquals(-71.78, gateway.getCumulativeNetPnL(), 0.001);
    }

    @Test
    void testLiveDeductionAfterTradeClosure() {
        PaperTradeRepository repository = mock(PaperTradeRepository.class);
        when(repository.findAll()).thenReturn(List.of());

        PaperTradingGateway gateway = new PaperTradingGateway(repository, 10000.0);
        assertEquals(10000.0, gateway.getTotalCapital(), 0.001);

        // Buy 100 shares of ABC at 100
        gateway.placeOrder("ABC", "BUY", 100, 100.0, 0.0, "LIMIT");
        // Place stop loss at 98.0
        gateway.placeOrder("ABC", "SELL", 100, 97.8, 98.0, "SL");

        // Tick drops to SL limit price 97.8 -> triggers and fills exit
        gateway.onTick("ABC", 97.8);

        assertTrue(gateway.getOpenPosition().isEmpty());
        assertTrue(gateway.getTotalCapital() < 10000.0, "Capital must be deducted after loss");
        assertTrue(gateway.getCumulativeNetPnL() < 0, "Cumulative net PnL must be negative");
    }
}
