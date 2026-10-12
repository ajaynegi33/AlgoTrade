package com.algotrade.algotradebackend.strategy.momentum_breakout;

import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.*;

public class CandidateMetricsTest {

    @Test
    void testBaselineVolumeDoesNotDistortVwapAndWindowVolume() {
        CandidateMetrics m = new CandidateMetrics("RELIANCE", 1000.0, 1010.0, 5_000_000, 2.0);

        // First tick arrives at 09:15 with 100,000 pre-market cumulative volume
        m.updateTick(1005.0, 100_000, 50_000, 20_000);

        // Window volume should be 0 because 100,000 was the baseline
        assertEquals(0, m.getWindowVolume(), "Baseline tick must yield 0 window volume");
        // VWAP should default to LTP (1005.0) since no incremental volume has traded yet
        assertEquals(1005.0, m.getVwap(), 0.001, "VWAP on baseline tick should equal LTP");

        // Second tick arrives with 200 new shares traded at 1006.0
        m.updateTick(1006.0, 100_200, 60_000, 20_000);
        assertEquals(200, m.getWindowVolume(), "Window volume must be exactly 200");
        assertEquals(1006.0, m.getVwap(), 0.001, "VWAP must be exactly 1006.0 for 200 shares");

        // Third tick arrives with 800 shares traded at 1001.0
        m.updateTick(1001.0, 101_000, 70_000, 20_000);
        assertEquals(1000, m.getWindowVolume(), "Window volume must be 1000");
        // VWAP = (200 * 1006.0 + 800 * 1001.0) / 1000 = (201200 + 800800) / 1000 = 1002000 / 1000 = 1002.0
        assertEquals(1002.0, m.getVwap(), 0.001, "VWAP must accurately compute incremental volume");
    }

    @Test
    void testResetSessionClearsDailyObservations() {
        CandidateMetrics m = new CandidateMetrics("TCS", 3500.0, 3550.0, 2_000_000, 1.5);
        m.updateTick(3520.0, 50_000, 10_000, 5_000);
        m.updateTick(3540.0, 80_000, 15_000, 5_000);

        assertEquals(3520.0, m.getOpenPrice());
        assertEquals(3520.0, m.getLow15());
        assertEquals(3540.0, m.getHigh15());
        assertEquals(30_000, m.getWindowVolume());

        // Perform session reset for new day
        m.resetSession();

        assertEquals(0.0, m.getOpenPrice());
        assertEquals(0.0, m.getLtp());
        assertEquals(0.0, m.getHigh15());
        assertEquals(0, m.getWindowVolume());
        // Reference data preserved
        assertEquals(3500.0, m.getPrevClose());
        assertEquals(3550.0, m.getPreviousDayHigh());
    }

    @Test
    void testOpeningVolumeBaselineIncludesEarlySecondsVolume() {
        CandidateMetrics m = new CandidateMetrics("INFY", 1500.0, 1520.0, 4_000_000, 1.8);

        // Pre-open equilibrium at 09:08 establishes 50,000 baseline at 09:15:00
        m.setOpeningVolumeBaseline(50_000);

        // First tick arrives 5 seconds late at 09:15:05 with 65,000 cumulative volume traded
        m.updateTick(1510.0, 65_000, 100_000, 20_000);

        // Window volume must include the 15,000 shares traded between 09:15:00 and 09:15:05
        assertEquals(15_000, m.getWindowVolume(), "Volume traded before first tick must be included when baseline is set");
        assertEquals(1510.0, m.getVwap(), 0.001);
    }

    @Test
    void testAggregateCandlesComputesTypicalPriceVwapAndWindowVolume() {
        CandidateMetrics m = new CandidateMetrics("HDFCBANK", 1600.0, 1620.0, 8_000_000, 1.5);

        // Aggregate 1-minute candle: Open=1605, High=1615, Low=1600, Close=1610, Vol=100,000
        // Typical Price = (1615 + 1600 + 1610) / 3 = 4825 / 3 = 1608.3333
        m.aggregateCandle(1605.0, 1615.0, 1600.0, 1610.0, 100_000);

        assertEquals(100_000, m.getWindowVolume());
        assertEquals(1605.0, m.getOpenPrice());
        assertEquals(1600.0, m.getLow15());
        assertEquals(1615.0, m.getHigh15());
        assertEquals(1610.0, m.getLtp());
        assertEquals(1608.333, m.getVwap(), 0.01);
    }

    @Test
    void testExchangeVwapOverridesTickApproximation() {
        CandidateMetrics m = new CandidateMetrics("SBIN", 800.0, 810.0, 15_000_000, 2.2);

        // Official NSE exchange ATP is 806.45, while tick LTP is 808.0
        m.updateTick(808.0, 200_000, 500_000, 100_000, 806.45, true);

        assertEquals(806.45, m.getVwap(), 0.001, "Official exchange ATP must take precedence over tick approximation");
        assertEquals(806.45, m.getSnapshot().vwap(), 0.001);
    }
}
