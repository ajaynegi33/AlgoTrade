package com.algotrade.algotradebackend.core.model;

import java.math.BigDecimal;
import java.math.RoundingMode;

/**
 * Calculates and encapsulates the exact statutory taxes, regulatory fees, and brokerage
 * charges for National Stock Exchange (NSE) intraday equity cash trades.
 * 
 * <p>Formulas implemented per Zerodha's official equity charge sheet:</p>
 * <ul>
 *   <li><b>Brokerage:</b> ₹20 flat or 0.03% of order value (whichever is lower) per executed order.</li>
 *   <li><b>STT (Securities Transaction Tax):</b> 0.025% applied strictly to the Sell turnover.</li>
 *   <li><b>Exchange Transaction Charges (NSE):</b> 0.00297% of total turnover (Buy + Sell).</li>
 *   <li><b>SEBI Turnover Charges:</b> ₹10 per Crore (0.0001%) of total turnover.</li>
 *   <li><b>Stamp Duty:</b> 0.003% applied strictly to the Buy turnover.</li>
 *   <li><b>GST (Goods and Services Tax):</b> 18% applied on (Brokerage + Exchange Charges + SEBI Charges).</li>
 * </ul>
 */
public record ChargesBreakdown(
    double buyTurnover,
    double sellTurnover,
    double totalTurnover,
    double brokerage,
    double stt,
    double exchangeCharges,
    double sebiCharges,
    double stampDuty,
    double gst,
    double totalCharges,
    double grossPnL,
    double netPnL
) {

    /**
     * Factory method to calculate the exact breakdown given execution parameters.
     * 
     * @param quantity Number of shares traded
     * @param buyPrice Average executed buy price per share
     * @param sellPrice Average executed sell price per share
     * @return A complete ChargesBreakdown instance rounded to 2 decimal places (paise)
     */
    public static ChargesBreakdown calculate(int quantity, double buyPrice, double sellPrice) {
        double buyTurnover = round(quantity * buyPrice);
        double sellTurnover = round(quantity * sellPrice);
        double totalTurnover = round(buyTurnover + sellTurnover);

        // 1. Brokerage: min(₹20, 0.03% of turnover) for buy, and same for sell
        double buyBrokerage = Math.min(20.00, round(buyTurnover * 0.0003));
        double sellBrokerage = Math.min(20.00, round(sellTurnover * 0.0003));
        double brokerage = round(buyBrokerage + sellBrokerage);

        // 2. STT: 0.025% on Sell Turnover only for intraday equity
        double stt = round(sellTurnover * 0.00025);

        // 3. Exchange Transaction Charges (NSE Equity): 0.00297% on total turnover
        double exchangeCharges = round(totalTurnover * 0.0000297);

        // 4. SEBI Turnover Charges: ₹10 per crore (0.0001% of total turnover)
        double sebiCharges = round(totalTurnover * 0.000001);

        // 5. Stamp Duty: 0.003% on Buy Turnover only
        double stampDuty = round(buyTurnover * 0.00003);

        // 6. GST: 18% on (Brokerage + Exchange Charges + SEBI Charges)
        double gstTaxableBase = brokerage + exchangeCharges + sebiCharges;
        double gst = round(gstTaxableBase * 0.18);

        // Total Statutory & Brokerage Deductions
        double totalCharges = round(brokerage + stt + exchangeCharges + sebiCharges + stampDuty + gst);

        // Gross Profit or Loss = (Sell Value - Buy Value)
        double grossPnL = round(sellTurnover - buyTurnover);

        // True Net Take-Home PnL = Gross PnL - Total Deductions
        double netPnL = round(grossPnL - totalCharges);

        return new ChargesBreakdown(
            buyTurnover,
            sellTurnover,
            totalTurnover,
            brokerage,
            stt,
            exchangeCharges,
            sebiCharges,
            stampDuty,
            gst,
            totalCharges,
            grossPnL,
            netPnL
        );
    }

    /**
     * Helper to round double values to 2 decimal places using HALF_UP rounding.
     */
    private static double round(double value) {
        return BigDecimal.valueOf(value)
            .setScale(2, RoundingMode.HALF_UP)
            .doubleValue();
    }
}
