package com.algotrade.algotradebackend.core.util;

import java.math.BigDecimal;
import java.math.RoundingMode;

/**
 * High-precision mathematical rounding and truncation engine aligned to exchange tick increments.
 * Prevents IEEE-754 floating point precision drift in Indian equity trading (e.g. ₹0.01, ₹0.05, ₹0.10 ticks).
 */
public final class TickMath {

    private TickMath() {}

    /**
     * Resolves the authoritative exchange tick size per NSE Circular CMTR62174:
     * - Equity price <= ₹250.00: ₹0.01 tick size
     * - Equity price > ₹250.00: ₹0.05 tick size
     */
    public static double getExchangeTickSize(double price) {
        return (price > 0.0 && price <= 250.0) ? 0.01 : 0.05;
    }

    /**
     * Floors price down to the nearest valid exchange tick increment.
     * Normalizes floating point subtraction drift (e.g. 99.55 - 0.01 = 99.53999999999999) to 6 decimal places.
     */
    public static double floorToTick(double price, double tickSize) {
        if (tickSize <= 0.0) return price;
        BigDecimal p = BigDecimal.valueOf(price).setScale(6, RoundingMode.HALF_UP);
        BigDecimal ts = BigDecimal.valueOf(tickSize).setScale(6, RoundingMode.HALF_UP);
        return p.divide(ts, 0, RoundingMode.FLOOR)
                .multiply(ts)
                .setScale(4, RoundingMode.HALF_UP)
                .doubleValue();
    }

    /**
     * Ceils price up to the nearest valid exchange tick increment.
     * Normalizes floating point addition drift to 6 decimal places.
     */
    public static double ceilToTick(double price, double tickSize) {
        if (tickSize <= 0.0) return price;
        BigDecimal p = BigDecimal.valueOf(price).setScale(6, RoundingMode.HALF_UP);
        BigDecimal ts = BigDecimal.valueOf(tickSize).setScale(6, RoundingMode.HALF_UP);
        return p.divide(ts, 0, RoundingMode.CEILING)
                .multiply(ts)
                .setScale(4, RoundingMode.HALF_UP)
                .doubleValue();
    }

    /**
     * Rounds price to the nearest valid exchange tick using standard HALF_UP.
     */
    public static double roundToTick(double price, double tickSize) {
        if (tickSize <= 0.0) return price;
        BigDecimal p = BigDecimal.valueOf(price);
        BigDecimal ts = BigDecimal.valueOf(tickSize);
        return p.divide(ts, 0, RoundingMode.HALF_UP)
                .multiply(ts)
                .setScale(4, RoundingMode.HALF_UP)
                .doubleValue();
    }
}
