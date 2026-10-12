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

        // Gross Profit should be exactly ₹400.00
        assertEquals(400.00, charges.grossPnL());

        // Total charges must be positive and Net PnL must be Gross - Total Charges
        assertTrue(charges.totalCharges() > 0);
        assertEquals(charges.grossPnL() - charges.totalCharges(), charges.netPnL(), 0.01);

        // Net PnL must be strictly less than Gross PnL due to deductions
        assertTrue(charges.netPnL() < charges.grossPnL());
    }
}
