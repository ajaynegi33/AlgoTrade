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
    private long openingBaselineVolume = -1;
    private long accumulatedCandleVolume = 0;

    // VWAP Accumulators
    private double sumPriceVolume = 0.0;
    private long sumVolume = 0;
    private double exchangeVwap = 0.0;

    // Depth accumulators
    private long totalBuyQty = 0;
    private long totalSellQty = 0;

    // Real-Time Breakout Confirmation (filters 1-tick fakeouts)
    private final java.util.concurrent.atomic.AtomicInteger consecutiveBreakoutTicks = new java.util.concurrent.atomic.AtomicInteger(0);

    // Instrument Tick Size (from Kite Instrument Master / MarketUniverseEntity)
    private volatile double tickSize = 0.05;

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
        if (this.previousDayHigh > 0 && previousDayHigh > 0 && Math.abs(this.previousDayHigh - previousDayHigh) > 0.001) {
            // Guard: Once established for the day, do not clobber true PDH with intraday ticks
            return;
        }
        if (prevClose > 0) this.prevClose = prevClose;
        if (previousDayHigh > 0) this.previousDayHigh = previousDayHigh;
        if (avgDailyVolume10Day > 0) this.avgDailyVolume10Day = avgDailyVolume10Day;
        if (atrPct > 0) this.atrPct = atrPct;
    }

    /**
     * Explicitly establishes the reliable 09:15:00 window-boundary volume baseline
     * (e.g. from pre-open trade match volume or snapshot at 09:15:00.000).
     */
    public synchronized void setOpeningVolumeBaseline(long baselineVolume) {
        this.openingBaselineVolume = baselineVolume;
        if (this.currentVolume < baselineVolume) {
            this.currentVolume = baselineVolume;
        }
    }

    /**
     * Injects the official exchange-calculated trade-weighted VWAP (Average Traded Price / ATP).
     */
    public synchronized void setExchangeVwap(double vwap) {
        if (vwap > 0) {
            this.exchangeVwap = vwap;
        }
    }

    /**
     * Aggregates 1-minute candle data over the 09:15–09:30 observation window.
     * Guards against double-counting if live tick stream is already actively accumulating this session.
     */
    public synchronized void aggregateCandle(double open, double high, double low, double close, long candleVolume) {
        aggregateCandle(open, high, low, close, candleVolume, true);
    }

    /**
     * Aggregates candle data with explicit observation window gating.
     */
    public synchronized void aggregateCandle(double open, double high, double low, double close, long candleVolume, boolean inObservationWindow) {
        if (sumVolume > 0 && accumulatedCandleVolume == 0) {
            // Tick stream is already active and authoritative; avoid double counting candle volume
            return;
        }
        if (openPrice == 0.0) {
            this.openPrice = open;
        }
        this.ltp = close;
        if (inObservationWindow) {
            this.low15 = Math.min(this.low15, low);
            this.high15 = Math.max(this.high15, high);
        } else if (this.low15 == Double.MAX_VALUE) {
            this.low15 = low;
            this.high15 = high;
        }
        this.accumulatedCandleVolume += candleVolume;

        if (candleVolume > 0) {
            double typicalPrice = (high + low + close) / 3.0;
            this.sumPriceVolume += (typicalPrice * candleVolume);
            this.sumVolume += candleVolume;
        }
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
        this.openingBaselineVolume = -1;
        this.accumulatedCandleVolume = 0;
        this.sumPriceVolume = 0.0;
        this.sumVolume = 0;
        this.exchangeVwap = 0.0;
        this.totalBuyQty = 0;
        this.totalSellQty = 0;
        this.consecutiveBreakoutTicks.set(0);
    }

    /**
     * Ingests a live tick update.
     */
    public synchronized void updateTick(double price, long volume, long buyQty, long sellQty) {
        updateTick(price, volume, buyQty, sellQty, 0.0, true);
    }

    /**
     * Ingests a live tick update with observation window gating.
     */
    public synchronized void updateTick(double price, long volume, long buyQty, long sellQty, boolean inObservationWindow) {
        updateTick(price, volume, buyQty, sellQty, 0.0, inObservationWindow);
    }

    /**
     * Ingests a live tick update with official exchange-calculated VWAP (ATP) and observation window gating.
     * When inObservationWindow is false, only real-time LTP and order-book depth are updated;
     * 15-minute range (low15, high15) remains strictly locked to the 09:15–09:30 window.
     */
    public synchronized void updateTick(double price, long volume, long buyQty, long sellQty, double tickExchangeVwap, boolean inObservationWindow) {
        this.ltp = price;
        this.totalBuyQty = buyQty;
        this.totalSellQty = sellQty;

        // Continuously update official exchange-calculated VWAP (ATP) whenever available
        if (tickExchangeVwap > 0) {
            this.exchangeVwap = tickExchangeVwap;
        }

        if (openPrice == 0.0) {
            this.openPrice = price;
        }

        // Lock 15-minute range strictly to the 09:15–09:30 observation window
        if (inObservationWindow) {
            this.low15 = Math.min(this.low15, price);
            this.high15 = Math.max(this.high15, price);
        } else if (this.low15 == Double.MAX_VALUE) {
            this.low15 = price;
            this.high15 = price;
        }

        if (this.initialVolume == -1) {
            // Baseline calibration: If reliable window boundary baseline is set, use it
            this.initialVolume = (this.openingBaselineVolume >= 0) ? this.openingBaselineVolume : volume;
            this.currentVolume = volume;
            long openingDelta = Math.max(0, volume - this.initialVolume);
            if (openingDelta > 0) {
                this.sumPriceVolume += (price * openingDelta);
                this.sumVolume += openingDelta;
            }
        } else {
            long incrementalVol = Math.max(0, volume - this.currentVolume);
            this.currentVolume = volume;

            if (incrementalVol > 0) {
                this.sumPriceVolume += (price * incrementalVol);
                this.sumVolume += incrementalVol;
            }
        }
    }

    /**
     * Captures an immutable, thread-safe point-in-time snapshot of metrics under lock.
     */
    public synchronized CandidateSnapshot getSnapshot() {
        double currentVwap = getVwap();
        long windowVol = getWindowVolume();
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
        if (exchangeVwap > 0) return exchangeVwap;
        if (sumVolume <= 0) return ltp;
        return sumPriceVolume / sumVolume;
    }

    public synchronized long getWindowVolume() {
        if (accumulatedCandleVolume > 0) return accumulatedCandleVolume;
        if (openingBaselineVolume >= 0) return Math.max(0, currentVolume - openingBaselineVolume);
        if (initialVolume == -1) return 0;
        return Math.max(0, currentVolume - initialVolume);
    }

    public synchronized double getDepthImbalance() {
        long total = totalBuyQty + totalSellQty;
        if (total <= 0) return 0.0;
        return (double) (totalBuyQty - totalSellQty) / total;
    }

    public int incrementBreakoutTicks() {
        return consecutiveBreakoutTicks.incrementAndGet();
    }

    public void resetBreakoutTicks() {
        consecutiveBreakoutTicks.set(0);
    }

    public int getConsecutiveBreakoutTicks() {
        return consecutiveBreakoutTicks.get();
    }

    public double getTickSize() {
        return tickSize > 0.0 ? tickSize : 0.05;
    }

    public void setTickSize(double tickSize) {
        if (tickSize > 0.0) {
            this.tickSize = tickSize;
        }
    }
}
