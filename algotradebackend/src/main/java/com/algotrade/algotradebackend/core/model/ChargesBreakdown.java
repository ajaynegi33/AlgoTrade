package com.algotrade.algotradebackend.core.model;

import java.math.BigDecimal;
import java.math.RoundingMode;
import java.util.Optional;
import com.algotrade.algotradebackend.core.util.TickMath;

/**
 * Calculates and encapsulates the exact statutory taxes, regulatory fees, and brokerage
 * charges for National Stock Exchange (NSE) intraday equity cash trades.
 * 
 * <p>Formulas implemented per Zerodha's official equity charge sheet:</p>
 * <ul>
 *   <li><b>Brokerage:</b> ₹20 flat or 0.03% of order value (whichever is lower) per executed order.</li>
 *   <li><b>STT (Securities Transaction Tax):</b> 0.025% applied strictly to the Sell turnover.</li>
 *   <li><b>Exchange Transaction Charges (NSE):</b> 0.00307% of total turnover (Buy + Sell).</li>
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

        // 3. Exchange Transaction Charges (NSE Equity): 0.00307% on total turnover
        double exchangeCharges = round(totalTurnover * 0.0000307);

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
     * Computes the exact minimum exit price aligned to the instrument's tick size
     * that secures at least ₹10.00 Net Take-Home P&L after all statutory taxes,
     * exchange fees, SEBI charges, stamp duty, GST, and brokerages.
     * 
     * <p>Algorithm is fail-closed: it analytically estimates the required gross profit,
     * rounds up to the valid exchange tick size, and verifies the candidate against
     * the actual {@link #calculate(int, double, double)} formula. If not achieved within
     * 10 tick steps or if candidate exceeds +1.5% from entry, returns {@link Optional#empty()}.</p>
     * 
     * @param quantity Number of shares filled
     * @param filledBuyPrice Confirmed executed buy price
     * @param tickSize Exchange instrument tick size (e.g. 0.01, 0.05, 0.10)
     * @return Valid tick-aligned exit price guaranteeing netPnL >= 10.00, or empty if impossible
     */
    public static Optional<Double> calculateExactBreakevenPrice(int quantity, double filledBuyPrice, double tickSize) {
        if (quantity <= 0 || filledBuyPrice <= 0.0 || tickSize <= 0.0) {
            return Optional.empty();
        }

        // 1. Analytical baseline estimation:
        // Evaluate an initial candidate around +0.20% to get an authoritative charge estimate.
        ChargesBreakdown estBreakdown = calculate(quantity, filledBuyPrice, filledBuyPrice * 1.0020);
        double targetGrossProfit = estBreakdown.totalCharges() + 10.00;
        double priceDeltaPerShare = targetGrossProfit / quantity;
        double candidate = TickMath.ceilToTick(filledBuyPrice + priceDeltaPerShare, tickSize);

        // 2. Exact verification loop:
        // Check if candidate yields netPnL >= 10.00. Step up by tick if needed (up to 10 ticks max).
        double maxAllowedCandidate = filledBuyPrice * 1.015; // Safeguard: never exceed +1.5% for breakeven
        for (int step = 0; step < 10 && candidate <= maxAllowedCandidate; step++) {
            ChargesBreakdown verified = calculate(quantity, filledBuyPrice, candidate);
            if (verified.netPnL() >= 10.00) {
                return Optional.of(candidate);
            }
            candidate = TickMath.ceilToTick(candidate + tickSize, tickSize);
        }

        return Optional.empty();
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
