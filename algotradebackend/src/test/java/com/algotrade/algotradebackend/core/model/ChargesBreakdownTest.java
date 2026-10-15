package com.algotrade.algotradebackend.core.model;

import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

/**
 * Runnable self-check for the Indian statutory equity charges and PnL calculator.
 */
public class ChargesBreakdownTest {

    @Test
    void testChargesCalculationAccuracy() {
        // Buy 100 shares @ ₹200 (Buy Turnover = ₹20,000)
        // Sell 100 shares @ ₹204 (Sell Turnover = ₹20,400)
        // Gross Profit = ₹400.00
        ChargesBreakdown charges = ChargesBreakdown.calculate(100, 200.0, 204.0);

        assertEquals(20000.00, charges.buyTurnover());
        assertEquals(20400.00, charges.sellTurnover());
        assertEquals(40400.00, charges.totalTurnover());

        // Brokerage: min(20, 20000 * 0.0003 = 6.00) + min(20, 20400 * 0.0003 = 6.12) = 12.12
        assertEquals(12.12, charges.brokerage());

        // STT: 20400 * 0.00025 = 5.10
        assertEquals(5.10, charges.stt());

        // Exchange Transaction Charges: 40400 * 0.0000307 = 1.24
        assertEquals(1.24, charges.exchangeCharges());

        // Gross Profit should be exactly ₹400.00
        assertEquals(400.00, charges.grossPnL());

        // Total charges must be positive and Net PnL must be Gross - Total Charges
        assertTrue(charges.totalCharges() > 0);
        assertEquals(charges.grossPnL() - charges.totalCharges(), charges.netPnL(), 0.01);

        // Net PnL must be strictly less than Gross PnL due to deductions
        assertTrue(charges.netPnL() < charges.grossPnL());
    }

    @Test
    void testExactBreakevenForThreePricingTiers() {
        // Tier 1: ₹100 stock, 500 shares, ₹0.01 tick
        var be100 = ChargesBreakdown.calculateExactBreakevenPrice(500, 100.0, 0.01);
        assertTrue(be100.isPresent());
        assertEquals(100.13, be100.get(), 0.001);
        ChargesBreakdown ch100 = ChargesBreakdown.calculate(500, 100.0, be100.get());
        assertTrue(ch100.netPnL() >= 10.00, "Net PnL must be >= ₹10.00, got: " + ch100.netPnL());

        // Tier 2: ₹500 stock, 100 shares, ₹0.05 tick
        var be500 = ChargesBreakdown.calculateExactBreakevenPrice(100, 500.0, 0.05);
        assertTrue(be500.isPresent());
        assertEquals(500.65, be500.get(), 0.001);
        ChargesBreakdown ch500 = ChargesBreakdown.calculate(100, 500.0, be500.get());
        assertTrue(ch500.netPnL() >= 10.00, "Net PnL must be >= ₹10.00, got: " + ch500.netPnL());

        // Tier 3: ₹1,500 stock, 33 shares, ₹0.10 tick
        var be1500 = ChargesBreakdown.calculateExactBreakevenPrice(33, 1500.0, 0.10);
        assertTrue(be1500.isPresent());
        assertEquals(1501.90, be1500.get(), 0.001);
        ChargesBreakdown ch1500 = ChargesBreakdown.calculate(33, 1500.0, be1500.get());
        assertTrue(ch1500.netPnL() >= 10.00, "Net PnL must be >= ₹10.00, got: " + ch1500.netPnL());
    }

    @Test
    void testExactBreakevenInvalidInputs() {
        assertTrue(ChargesBreakdown.calculateExactBreakevenPrice(0, 100.0, 0.01).isEmpty());
        assertTrue(ChargesBreakdown.calculateExactBreakevenPrice(10, 0.0, 0.01).isEmpty());
        assertTrue(ChargesBreakdown.calculateExactBreakevenPrice(10, 100.0, -0.05).isEmpty());
    }
}
