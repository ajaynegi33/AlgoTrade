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
}
