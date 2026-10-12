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
     * Resets observation metrics for a fresh trading day session while preserving
     * static/reference data (symbol, prevClose, PDH, avgDailyVolume, atrPct).
     */
    public synchronized void resetSession() {
        this.openPrice = 0.0;
        this.ltp = 0.0;
        this.low15 = Double.MAX_VALUE;
        this.high15 = 0.0;
        this.initialVolume = -1;
        this.currentVolume = 0;
        this.sumPriceVolume = 0.0;
        this.sumVolume = 0;
        this.totalBuyQty = 0;
        this.totalSellQty = 0;
    }

    /**
     * Ingests a live tick update.
     */
    public synchronized void updateTick(double price, long volume, long buyQty, long sellQty) {
        updateTick(price, volume, buyQty, sellQty, true);
    }

    /**
     * Ingests a live tick update with observation window gating.
     * When inObservationWindow is false, only real-time LTP and order-book depth are updated;
     * 15-minute range (low15, high15, window volume, VWAP) remain locked to the 09:15–09:30 window.
     */
    public synchronized void updateTick(double price, long volume, long buyQty, long sellQty, boolean inObservationWindow) {
        this.ltp = price;
        this.totalBuyQty = buyQty;
        this.totalSellQty = sellQty;

        if (inObservationWindow) {
            if (openPrice == 0.0) {
                this.openPrice = price;
            }

            this.low15 = Math.min(this.low15, price);
            this.high15 = Math.max(this.high15, price);

            if (this.initialVolume == -1) {
                // Baseline calibration: first tick sets the starting volume baseline.
                // Do NOT count entire historical/pre-market volume as incremental intraday tick volume.
                this.initialVolume = volume;
                this.currentVolume = volume;
            } else {
                long incrementalVol = Math.max(0, volume - this.currentVolume);
                this.currentVolume = volume;

                if (incrementalVol > 0) {
                    this.sumPriceVolume += (price * incrementalVol);
                    this.sumVolume += incrementalVol;
                }
            }
        }
    }

    /**
     * Captures an immutable, thread-safe point-in-time snapshot of metrics under lock.
     */
    public synchronized CandidateSnapshot getSnapshot() {
        double currentVwap = sumVolume > 0 ? (sumPriceVolume / sumVolume) : ltp;
        long windowVol = initialVolume == -1 ? 0 : Math.max(0, currentVolume - initialVolume);
        long totalDepth = totalBuyQty + totalSellQty;
        double depthImbalance = totalDepth > 0 ? (double) (totalBuyQty - totalSellQty) / totalDepth : 0.0;
        double gap = prevClose > 0 ? ((openPrice - prevClose) / prevClose) * 100.0 : 0.0;
        double effectiveLow = (low15 == Double.MAX_VALUE) ? ltp : low15;

        return new CandidateSnapshot(
            symbol,
            ltp,
            openPrice,
            effectiveLow,
            high15,
            prevClose,
            previousDayHigh,
            avgDailyVolume10Day,
            atrPct,
            windowVol,
            currentVwap,
            depthImbalance,
            gap
        );
    }

    public synchronized String getSymbol() { return symbol; }
    public synchronized double getPrevClose() { return prevClose; }
    public synchronized double getPreviousDayHigh() { return previousDayHigh; }
    public synchronized double getAvgDailyVolume10Day() { return avgDailyVolume10Day; }
    public synchronized double getAtrPct() { return atrPct; }
    public synchronized double getOpenPrice() { return openPrice; }
    public synchronized double getLtp() { return ltp; }
    public synchronized double getLow15() { return low15 == Double.MAX_VALUE ? ltp : low15; }
    public synchronized double getHigh15() { return high15; }

    public synchronized double getGapPct() {
        if (prevClose <= 0) return 0.0;
        return ((openPrice - prevClose) / prevClose) * 100.0;
    }

    public synchronized double getVwap() {
        if (sumVolume <= 0) return ltp;
        return sumPriceVolume / sumVolume;
    }

    public synchronized long getWindowVolume() {
        if (initialVolume == -1) return 0;
        return Math.max(0, currentVolume - initialVolume);
    }

    public synchronized double getDepthImbalance() {
        long total = totalBuyQty + totalSellQty;
        if (total <= 0) return 0.0;
        return (double) (totalBuyQty - totalSellQty) / total;
    }
}
