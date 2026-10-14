package com.algotrade.algotradebackend.core.util;

import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

class TickMathTest {

    @Test
    void testOnePaisaTickCalculations() {
        double tickSize = 0.01;
        // Entry ₹100.00, Support ₹99.55 -> Trigger ₹99.54
        double trigger = TickMath.floorToTick(99.55 - tickSize, tickSize);
        assertEquals(99.54, trigger, 0.0001);

        // Limit at 0.15% below trigger: 99.54 * 0.9985 = 99.39069 -> 99.39
        double limit = TickMath.floorToTick(trigger * 0.9985, tickSize);
        assertEquals(99.39, limit, 0.0001);

        // Ceil test
        assertEquals(100.06, TickMath.ceilToTick(100.05001, tickSize), 0.0001);
        assertEquals(100.05, TickMath.ceilToTick(100.05, tickSize), 0.0001);
    }

    @Test
    void testFivePaiseTickCalculations() {
        double tickSize = 0.05;
        // Entry ₹500.00, Support ₹497.50 -> Trigger ₹497.45
        double trigger = TickMath.floorToTick(497.50 - tickSize, tickSize);
        assertEquals(497.45, trigger, 0.0001);

        // Limit at 0.15% below trigger: 497.45 * 0.9985 = 496.7038 -> 496.70
        double limit = TickMath.floorToTick(trigger * 0.9985, tickSize);
        assertEquals(496.70, limit, 0.0001);

        // Ceil test
        assertEquals(500.65, TickMath.ceilToTick(500.601, tickSize), 0.0001);
        assertEquals(500.65, TickMath.ceilToTick(500.65, tickSize), 0.0001);
    }

    @Test
    void testTenPaiseTickCalculations() {
        double tickSize = 0.10;
        // Entry ₹1,500.00, Support ₹1,492.00 -> Trigger ₹1,491.90
        double trigger = TickMath.floorToTick(1492.00 - tickSize, tickSize);
        assertEquals(1491.90, trigger, 0.0001);

        // Limit at 0.15% below trigger: 1491.90 * 0.9985 = 1489.66215 -> 1489.60
        double limit = TickMath.floorToTick(trigger * 0.9985, tickSize);
        assertEquals(1489.60, limit, 0.0001);

        // Ceil test
        assertEquals(1501.90, TickMath.ceilToTick(1501.81, tickSize), 0.0001);
        assertEquals(1501.90, TickMath.ceilToTick(1501.90, tickSize), 0.0001);
    }

    @Test
    void testInvalidTickSizeFallback() {
        // Safe passthrough if tick size is zero or negative
        assertEquals(123.456, TickMath.floorToTick(123.456, 0.0));
        assertEquals(123.456, TickMath.ceilToTick(123.456, -0.05));
    }
}
