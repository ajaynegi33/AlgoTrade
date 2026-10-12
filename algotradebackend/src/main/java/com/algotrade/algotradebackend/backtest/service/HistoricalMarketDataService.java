package com.algotrade.algotradebackend.backtest.service;

import com.algotrade.algotradebackend.strategy.momentum_breakout.CandidateMetrics;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;

import java.time.LocalDate;
import java.util.*;

/**
 * Provides historical market data and deterministic intraday candle series
 * for backtesting the Momentum Breakout Strategy across the Nifty universe.
 */
@Service
public class HistoricalMarketDataService {

    private static final Logger log = LoggerFactory.getLogger(HistoricalMarketDataService.class);

    public static final List<String> NIFTY_UNIVERSE = List.of(
        "RELIANCE", "TCS", "INFY", "HDFCBANK", "ICICIBANK",
        "SBIN", "BHARTIARTL", "LT", "AXISBANK", "NTPC",
        "WIPRO", "MARUTI", "SUNPHARMA", "BAJFINANCE", "TITAN",
        "ULTRACEMCO", "ASIANPAINT", "KOTAKBANK", "ITC", "TATACONSUM"
    );

    private static final Map<String, Double> BASE_PRICES = Map.ofEntries(
        Map.entry("RELIANCE", 1250.0),
        Map.entry("TCS", 3850.0),
        Map.entry("INFY", 1820.0),
        Map.entry("HDFCBANK", 1680.0),
        Map.entry("ICICIBANK", 1240.0),
        Map.entry("SBIN", 790.0),
        Map.entry("BHARTIARTL", 1650.0),
        Map.entry("LT", 3520.0),
        Map.entry("AXISBANK", 1180.0),
        Map.entry("NTPC", 385.0),
        Map.entry("WIPRO", 520.0),
        Map.entry("MARUTI", 12100.0),
        Map.entry("SUNPHARMA", 1860.0),
        Map.entry("BAJFINANCE", 6950.0),
        Map.entry("TITAN", 3450.0),
        Map.entry("ULTRACEMCO", 11200.0),
        Map.entry("ASIANPAINT", 2420.0),
        Map.entry("KOTAKBANK", 1790.0),
        Map.entry("ITC", 495.0),
        Map.entry("TATACONSUM", 1080.0)
    );

    public record IntradayBar(String time, double high, double low, double close) {}

    /**
     * Generates or fetches candidate opening metrics for all watchlist constituents on a given date.
     */
    public List<CandidateMetrics> getOpeningCandidateMetrics(LocalDate date) {
        List<CandidateMetrics> candidates = new ArrayList<>();

        for (String symbol : NIFTY_UNIVERSE) {
            long seed = symbol.hashCode() * 31L + date.toEpochDay() * 101L;
            Random rng = new Random(seed);

            double basePrice = BASE_PRICES.getOrDefault(symbol, 1000.0);
            // Daily drift from baseline
            double dateFactor = 1.0 + Math.sin(date.toEpochDay() * 0.05 + symbol.length()) * 0.08;
            double prevClose = Math.round(basePrice * dateFactor * 100.0) / 100.0;
            double pdh = Math.round(prevClose * (1.0 + 0.008 + rng.nextDouble() * 0.015) * 100.0) / 100.0;
            double avgDailyVol = 1_500_000 + rng.nextInt(3_000_000);
            double atrPct = 1.4 + rng.nextDouble() * 1.6; // 1.4% to 3.0%

            // Opening 09:15 to 09:30 candle
            // Occasional strong momentum breakouts (+1.5% to +3.2% gap)
            boolean isBreakoutDay = rng.nextDouble() < 0.22; // ~22% probability of gap up candidate
            double gapPct = isBreakoutDay ? (1.5 + rng.nextDouble() * 1.8) : (-0.8 + rng.nextDouble() * 2.0);
            double openPrice = Math.round(prevClose * (1.0 + gapPct / 100.0) * 100.0) / 100.0;

            // 09:15-09:30 range expansion
            double low15 = Math.min(openPrice, Math.round(openPrice * (1.0 - rng.nextDouble() * 0.006) * 100.0) / 100.0);
            double high15 = Math.max(openPrice, Math.round(openPrice * (1.0 + rng.nextDouble() * 0.012) * 100.0) / 100.0);
            double ltp930 = Math.round((low15 + (high15 - low15) * (0.4 + rng.nextDouble() * 0.55)) * 100.0) / 100.0;

            long windowVol = (long) (avgDailyVol * (isBreakoutDay ? (0.15 + rng.nextDouble() * 0.25) : (0.05 + rng.nextDouble() * 0.08)));
            double depthImbalance = isBreakoutDay ? (0.20 + rng.nextDouble() * 0.40) : (-0.20 + rng.nextDouble() * 0.35);

            CandidateMetrics m = new CandidateMetrics(symbol, prevClose, pdh, avgDailyVol, atrPct);
            // Simulate feed ingestion during opening window
            m.updateTick(openPrice, 1000, 1000, 1000);
            m.updateTick(low15, windowVol / 3, (long)(windowVol * (0.5 + depthImbalance/2)), (long)(windowVol * (0.5 - depthImbalance/2)));
            m.updateTick(high15, windowVol * 2 / 3, (long)(windowVol * (0.5 + depthImbalance/2)), (long)(windowVol * (0.5 - depthImbalance/2)));
            m.updateTick(ltp930, windowVol, (long)(windowVol * (0.5 + depthImbalance/2)), (long)(windowVol * (0.5 - depthImbalance/2)));

            candidates.add(m);
        }

        return candidates;
    }

    /**
     * Generates a realistic minute-by-minute trajectory (09:30 to 15:15) for an entered trade.
     */
    public List<IntradayBar> getIntradayTrajectory(LocalDate date, String symbol, double entryPrice, double initialSl) {
        List<IntradayBar> trajectory = new ArrayList<>();
        long seed = symbol.hashCode() * 73L + date.toEpochDay() * 59L;
        Random rng = new Random(seed);

        double currentPrice = entryPrice;
        // Intraday trend bias: ~60% of breakout trades trend upwards initially
        double trendBias = (rng.nextDouble() < 0.60) ? 0.00015 : -0.00012;

        int totalMinutes = 345; // 09:30 to 15:15 is 345 minutes
        for (int m = 1; m <= totalMinutes; m++) {
            int hour = 9 + (30 + m) / 60;
            int minute = (30 + m) % 60;
            String timeStr = String.format("%02d:%02d", hour, minute);

            // Volatility curve: higher volatility in morning & late afternoon
            double volMultiplier = (m < 90 || m > 300) ? 0.0012 : 0.0006;
            double stepReturn = trendBias + (rng.nextGaussian() * volMultiplier);
            double nextPrice = Math.max(1.0, currentPrice * (1.0 + stepReturn));

            double barHigh = Math.max(currentPrice, nextPrice) * (1.0 + rng.nextDouble() * 0.0004);
            double barLow = Math.min(currentPrice, nextPrice) * (1.0 - rng.nextDouble() * 0.0004);
            double barClose = Math.round(nextPrice * 100.0) / 100.0;

            trajectory.add(new IntradayBar(
                timeStr,
                Math.round(barHigh * 100.0) / 100.0,
                Math.round(barLow * 100.0) / 100.0,
                barClose
            ));

            currentPrice = nextPrice;
        }

        return trajectory;
    }
}
