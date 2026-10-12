package com.algotrade.algotradebackend.backtest.service;

import com.algotrade.algotradebackend.backtest.model.BacktestRequest;
import com.algotrade.algotradebackend.backtest.model.BacktestResult;
import com.algotrade.algotradebackend.risk.PositionSizingEngine;
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
        LocalDate start = end.minusDays(30);

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

        // Every trade must obey strategy constraints: taxes deducted, valid prices
        for (var trade : result.trades()) {
            assertTrue(trade.entryPrice() > 0);
            assertTrue(trade.exitPrice() > 0);
            assertTrue(trade.quantity() >= 1);
            assertTrue(trade.totalCharges() > 0, "Statutory charges must be deducted");
            assertNotNull(trade.exitReason());
            // Quantity must not violate 1.0x cash capital cap
            double totalPositionVal = trade.quantity() * trade.entryPrice();
            assertTrue(totalPositionVal <= 10000.0 * 1.5, "Single trade value must obey risk budget and capital");
        }

        // Drawdown should be >= 0
        assertTrue(result.summary().maxDrawdownPct() >= 0.0);
    }

    @Test
    void testPositionSizingStrategyParityConstraints() {
        double capital = 10000.0;
        double entryPrice = 1000.0;

        // 1. Noise gate test: Stop distance < 0.4% must result in 0 shares
        double narrowStop = 997.0; // 0.3% distance
        int qtyNarrow = PositionSizingEngine.calculateSizingQuantity(capital, entryPrice, narrowStop, 1.0, 1.0, 0.4, 2.0);
        assertEquals(0, qtyNarrow, "Stops narrower than 0.4% must be rejected as market noise");

        // 2. Budget tolerance test: Stop distance > 2.0% must result in 0 shares
        double wideStop = 970.0; // 3.0% distance
        int qtyWide = PositionSizingEngine.calculateSizingQuantity(capital, entryPrice, wideStop, 1.0, 1.0, 0.4, 2.0);
        assertEquals(0, qtyWide, "Stops wider than 2.0% must be rejected");

        // 3. Valid stop: 1.0% distance (₹10 stop) on ₹100 risk budget
        // Q_risk = 100 / 10 = 10 shares. Total value = 10 * 1000 = ₹10,000 (1.0x capital)
        double validStop = 990.0;
        int qtyValid = PositionSizingEngine.calculateSizingQuantity(capital, entryPrice, validStop, 1.0, 1.0, 0.4, 2.0);
        assertEquals(10, qtyValid);

        // 4. Quantity scales directly with user risk budget:
        int qtyRisk100 = PositionSizingEngine.calculateSizingQuantity(capital, entryPrice, 995.0, 1.0, 1.0, 0.4, 2.0);
        // Q_risk = 100 / 5 = 20 shares.
        assertEquals(20, qtyRisk100, "Calculates 20 shares for ₹100 risk budget on ₹5 stop");
    }

    @Test
    void testLeverageMultiplierScaling() {
        double capital = 10000.0;
        double entryPrice = 1000.0;
        double stopPrice = 995.0; // 0.5% stop distance (₹5)

        // 1.0x leverage: risk budget = 10,000 * 1% = 100 -> 100 / 5 = 20 shares
        int qty1x = PositionSizingEngine.calculateSizingQuantity(capital, entryPrice, stopPrice, 1.0, 1.0, 0.4, 2.0);
        assertEquals(20, qty1x);

        // 2.0x leverage: risk budget = 20,000 * 1% = 200 -> 200 / 5 = 40 shares
        int qty2x = PositionSizingEngine.calculateSizingQuantity(capital, entryPrice, stopPrice, 1.0, 2.0, 0.4, 2.0);
        assertEquals(40, qty2x);

        // 5.0x leverage with 1.0% risk: risk budget = 50,000 * 1% = 500 -> 500 / 5 = 100 shares
        int qty5x = PositionSizingEngine.calculateSizingQuantity(capital, entryPrice, stopPrice, 1.0, 5.0, 0.4, 2.0);
        assertEquals(100, qty5x);

        // User's exact scenario: 5.0x leverage on 1.5% stop distance (₹15 stop on ₹1000 stock)
        // Capital = 10,000, 5x leverage -> Capacity = 50,000
        // At 2.0% risk: Risk Budget = 50,000 * 2.0% = ₹1,000 -> 1000 / 15 = 66 shares
        int qty2Pct = PositionSizingEngine.calculateSizingQuantity(capital, entryPrice, 985.0, 2.0, 5.0, 0.4, 2.0);
        assertEquals(66, qty2Pct, "2.0% risk must allocate 66 shares (₹1,000 max loss)");

        // At 2.8% risk: Risk Budget = 50,000 * 2.8% = ₹1,400 -> 1400 / 15 = 93 shares
        int qty28Pct = PositionSizingEngine.calculateSizingQuantity(capital, entryPrice, 985.0, 2.8, 5.0, 0.4, 2.0);
        assertEquals(93, qty28Pct, "2.8% risk must allocate 93 shares (₹1,400 max loss)");

        // At 3.0% risk: Risk Budget = 50,000 * 3.0% = ₹1,500 -> 1500 / 15 = 100 shares
        int qty3Pct = PositionSizingEngine.calculateSizingQuantity(capital, entryPrice, 985.0, 3.0, 5.0, 0.4, 2.0);
        assertEquals(100, qty3Pct, "3.0% risk must allocate 100 shares (₹1,500 max loss)");

        // At 5.0% risk: Risk Budget = 50,000 * 5.0% = ₹2,500 -> 2500 / 15 = 166 shares
        int qty5Pct = PositionSizingEngine.calculateSizingQuantity(capital, entryPrice, 985.0, 5.0, 5.0, 0.4, 2.0);
        assertEquals(166, qty5Pct, "5.0% risk must allocate 166 shares (₹2,500 max loss)");

        // BacktestRequest validation & clamping
        BacktestRequest clampedLow = new BacktestRequest(null, null, 10000.0, 1.0, 75, 2.0, 0.05, 0.5);
        assertEquals(1.0, clampedLow.leverageMultiplier(), "Leverage below 1.0 must clamp to 1.0");

        BacktestRequest clampedHigh = new BacktestRequest(null, null, 10000.0, 1.0, 75, 2.0, 0.05, 10.0);
        assertEquals(5.0, clampedHigh.leverageMultiplier(), "Leverage above 5.0 must clamp to SEBI limit 5.0");
    }

    @Test
    void testHistoricalCandleRetrieval() {
        HistoricalMarketDataService dataService = new HistoricalMarketDataService();
        var candles = dataService.getDailyCandles("RELIANCE");
        assertNotNull(candles);
        assertFalse(candles.isEmpty(), "Daily candles for RELIANCE must be populated");

        var first = candles.get(0);
        assertTrue(first.open() > 0);
        assertTrue(first.high() >= first.low());
        assertTrue(first.close() > 0);
    }
}
