package com.algotrade.algotradebackend.backtest.service;

import com.algotrade.algotradebackend.backtest.model.BacktestRequest;
import com.algotrade.algotradebackend.backtest.model.BacktestResult;
import com.algotrade.algotradebackend.strategy.momentum_breakout.MomentumScoringEngine;
import org.junit.jupiter.api.Test;

import java.time.LocalDate;

import static org.junit.jupiter.api.Assertions.*;

class BacktestEngineServiceTest {

    @Test
    void testBacktestSimulationRun() {
        MomentumScoringEngine scoringEngine = new MomentumScoringEngine();
        HistoricalMarketDataService dataService = new HistoricalMarketDataService();
        BacktestEngineService backtestEngine = new BacktestEngineService(scoringEngine, dataService);

        LocalDate end = LocalDate.now();
        LocalDate start = end.minusDays(45);

        BacktestRequest request = new BacktestRequest(
            start.toString(),
            end.toString(),
            10000.0,
            1.0,
            75,
            2.0,
            0.05
        );

        BacktestResult result = backtestEngine.runBacktest(request);

        assertNotNull(result);
        assertNotNull(result.summary());
        assertNotNull(result.equityCurve());
        assertNotNull(result.trades());

        assertEquals(10000.0, result.summary().initialCapital());
        assertTrue(result.equityCurve().size() > 0);

        // Every trade must have valid entry and exit prices and taxes
        for (var trade : result.trades()) {
            assertTrue(trade.entryPrice() > 0);
            assertTrue(trade.exitPrice() > 0);
            assertTrue(trade.quantity() >= 1);
            assertTrue(trade.totalCharges() > 0, "Statutory charges must be deducted");
            assertNotNull(trade.exitReason());
        }

        // Drawdown should be >= 0
        assertTrue(result.summary().maxDrawdownPct() >= 0.0);
    }
}
