package com.algotrade.algotradebackend.strategy.momentum_breakout;

/**
 * Tracks and accumulates price, volume, and order-book depth metrics for a candidate stock
 * during the 09:15:00 to 09:30:00 AM observation window.
 */
public class CandidateMetrics {

    private final String symbol;
    private double prevClose = 0.0;
    private double previousDayHigh = 0.0;
    private double avgDailyVolume10Day = 0.0;
    private double atrPct = 0.0;

    private double openPrice = 0.0;
    private double ltp = 0.0;
    private double low15 = Double.MAX_VALUE;
    private double high15 = 0.0;

    private long initialVolume = -1;
    private long currentVolume = 0;

    // VWAP Accumulators
    private double sumPriceVolume = 0.0;
    private long sumVolume = 0;

    // Depth accumulators
    private long totalBuyQty = 0;
    private long totalSellQty = 0;

    public CandidateMetrics(String symbol) {
        this.symbol = symbol;
    }

    public CandidateMetrics(String symbol, double prevClose, double previousDayHigh, double avgDailyVolume10Day, double atrPct) {
        this.symbol = symbol;
        this.prevClose = prevClose;
        this.previousDayHigh = previousDayHigh;
        this.avgDailyVolume10Day = avgDailyVolume10Day;
        this.atrPct = atrPct;
    }

    /**
     * Dynamically updates reference figures fetched from the broker market data API.
     */
    public synchronized void setReferenceData(double prevClose, double previousDayHigh, double avgDailyVolume10Day, double atrPct) {
        this.prevClose = prevClose;
        this.previousDayHigh = previousDayHigh;
        this.avgDailyVolume10Day = avgDailyVolume10Day;
        this.atrPct = atrPct;
    }

    /**
     * Ingests a live tick update.
     */
    public synchronized void updateTick(double price, long volume, long buyQty, long sellQty) {
        this.ltp = price;
        if (openPrice == 0.0) {
            this.openPrice = price;
        }

        this.low15 = Math.min(this.low15, price);
        this.high15 = Math.max(this.high15, price);

        if (this.initialVolume == -1) {
            this.initialVolume = volume;
        }

        long incrementalVol = Math.max(0, volume - this.currentVolume);
        this.currentVolume = volume;

        if (incrementalVol > 0) {
            this.sumPriceVolume += (price * incrementalVol);
            this.sumVolume += incrementalVol;
        }

        this.totalBuyQty = buyQty;
        this.totalSellQty = sellQty;
    }

    public String getSymbol() { return symbol; }
    public double getPrevClose() { return prevClose; }
    public double getPreviousDayHigh() { return previousDayHigh; }
    public double getAvgDailyVolume10Day() { return avgDailyVolume10Day; }
    public double getAtrPct() { return atrPct; }
    public double getOpenPrice() { return openPrice; }
    public double getLtp() { return ltp; }
    public double getLow15() { return low15 == Double.MAX_VALUE ? ltp : low15; }
    public double getHigh15() { return high15; }

    public double getGapPct() {
        if (prevClose <= 0) return 0.0;
        return ((openPrice - prevClose) / prevClose) * 100.0;
    }

    public double getVwap() {
        if (sumVolume <= 0) return ltp;
        return sumPriceVolume / sumVolume;
    }

    public long getWindowVolume() {
        if (initialVolume == -1) return 0;
        return Math.max(0, currentVolume - initialVolume);
    }

    public double getDepthImbalance() {
        long total = totalBuyQty + totalSellQty;
        if (total <= 0) return 0.0;
        return (double) (totalBuyQty - totalSellQty) / total;
    }
}
