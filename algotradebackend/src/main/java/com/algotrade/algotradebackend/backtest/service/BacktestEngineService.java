package com.algotrade.algotradebackend.backtest.service;

import com.algotrade.algotradebackend.backtest.model.*;
import com.algotrade.algotradebackend.core.model.ChargesBreakdown;
import com.algotrade.algotradebackend.core.model.ScoredCandidate;
import com.algotrade.algotradebackend.risk.PositionSizingEngine;
import com.algotrade.algotradebackend.strategy.momentum_breakout.CandidateMetrics;
import com.algotrade.algotradebackend.strategy.momentum_breakout.MomentumScoringEngine;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;

import java.time.LocalDate;
import java.time.format.DateTimeParseException;
import java.util.*;

/**
 * High-performance backtesting simulation engine for the 15-Min Opening Momentum Breakout Strategy.
 * 
 * <p>Directly reuses production quantitative models:
 * <ul>
 *   <li>{@link MomentumScoringEngine} for 100-point candidate scoring and deterministic tie-breaking.</li>
 *   <li>{@link PositionSizingEngine} for strict risk budget, stop distance gates, and 1.0x cash exposure cap.</li>
 *   <li>{@link ChargesBreakdown} for deducting all Indian statutory taxes &amp; charges.</li>
 * </ul>
 * </p>
 */
@Service
public class BacktestEngineService {

    private static final Logger log = LoggerFactory.getLogger(BacktestEngineService.class);

    private final MomentumScoringEngine scoringEngine;
    private final HistoricalMarketDataService dataService;
    private final com.algotrade.algotradebackend.core.service.MarketCalendarService marketCalendarService;

    public BacktestEngineService(
        MomentumScoringEngine scoringEngine,
        HistoricalMarketDataService dataService
    ) {
        this(scoringEngine, dataService, new com.algotrade.algotradebackend.core.service.MarketCalendarService());
    }

    @org.springframework.beans.factory.annotation.Autowired
    public BacktestEngineService(
        MomentumScoringEngine scoringEngine,
        HistoricalMarketDataService dataService,
        com.algotrade.algotradebackend.core.service.MarketCalendarService marketCalendarService
    ) {
        this.scoringEngine = scoringEngine;
        this.dataService = dataService;
        this.marketCalendarService = marketCalendarService;
    }

    public BacktestResult runBacktest(BacktestRequest req) {
        LocalDate start = parseDate(req.startDate(), LocalDate.now().minusDays(90));
        LocalDate end = parseDate(req.endDate(), LocalDate.now());

        if (start.isAfter(end)) {
            LocalDate tmp = start;
            start = end;
            end = tmp;
        }

        // Retrieve dynamic active F&O constituent watchlist
        List<String> watchlist = dataService.getWatchlistSymbols();

        // Pre-warm data cache for all universe stocks across the requested date range
        dataService.preloadUniverse(watchlist, start, end);

        double initialCapital = req.initialCapital();
        double currentCapital = initialCapital;
        double peakCapital = initialCapital;

        double maxDrawdownRupees = 0.0;
        double maxDrawdownPct = 0.0;
        int maxConsecutiveLosses = 0;
        int currentConsecutiveLosses = 0;

        List<BacktestEquityPoint> equityCurve = new ArrayList<>();
        List<BacktestTrade> trades = new ArrayList<>();
        List<Double> dailyReturns = new ArrayList<>();

        int tradeCounter = 1;
        List<String> timeSlots = generate5mTimeSlots();

        for (LocalDate date = start; !date.isAfter(end); date = date.plusDays(1)) {
            // Skip non-trading sessions (weekends and official NSE holidays)
            if (!marketCalendarService.isTradingDay(date)) {
                continue;
            }

            int dailyTrades = 0;
            int dailyLosses = 0;
            double dayStartCapital = currentCapital;

            // Initialize CandidateMetrics & preload 5m bars for all constituents on this date
            Map<String, CandidateMetrics> metricsMap = new HashMap<>();
            Map<String, List<HistoricalMarketDataService.IntradayBar>> barsMap = new HashMap<>();

            for (String sym : watchlist) {
                HistoricalMarketDataService.BaselineReference ref = dataService.getBaselineReference(sym, date);
                if (ref.prevClose() <= 0) continue;
                CandidateMetrics m = new CandidateMetrics(sym, ref.prevClose(), ref.pdh(), ref.avgDailyVol(), ref.atrPct());
                metricsMap.put(sym, m);
                barsMap.put(sym, dataService.getIntradayBars(sym, date));
            }

            if (metricsMap.isEmpty()) {
                continue;
            }

            // Intraday Session State
            boolean inTrade = false;
            String activeSym = null;
            int activeQty = 0;
            double entryPrice = 0.0;
            double initialSl = 0.0;
            double activeSl = 0.0;
            double highWaterMark = 0.0;
            String entryTime = null;
            int activeScore = 0;
            Set<String> tradedSymbolsToday = new HashSet<>();

            // Pre-index intraday bars into an O(1) timestamp lookup table before replaying the day
            Map<String, Map<String, HistoricalMarketDataService.IntradayBar>> indexedBarsMap = new HashMap<>(barsMap.size());
            for (Map.Entry<String, List<HistoricalMarketDataService.IntradayBar>> entry : barsMap.entrySet()) {
                Map<String, HistoricalMarketDataService.IntradayBar> timeMap = new HashMap<>(entry.getValue().size());
                for (HistoricalMarketDataService.IntradayBar bar : entry.getValue()) {
                    timeMap.put(bar.time(), bar);
                }
                indexedBarsMap.put(entry.getKey(), timeMap);
            }

            // Replay continuous 5-minute bars throughout the trading day (09:15 to 15:30)
            for (String timeStr : timeSlots) {
                java.time.LocalTime barTime = java.time.LocalTime.parse(timeStr);

                // 1. Accumulate price, volume, and order-book depth for all candidate stocks
                for (Map.Entry<String, List<HistoricalMarketDataService.IntradayBar>> entry : barsMap.entrySet()) {
                    String sym = entry.getKey();
                    CandidateMetrics m = metricsMap.get(sym);
                    if (m == null) continue;

                    Map<String, HistoricalMarketDataService.IntradayBar> timeMap = indexedBarsMap.get(sym);
                    HistoricalMarketDataService.IntradayBar bar = (timeMap != null) ? timeMap.get(timeStr) : null;
                    if (bar == null) continue;

                    boolean inObsWindow = !barTime.isBefore(java.time.LocalTime.of(9, 15)) 
                                       && barTime.isBefore(java.time.LocalTime.of(9, 30));
                    m.aggregateCandle(bar.open(), bar.high(), bar.low(), bar.close(), bar.volume(), inObsWindow);
                    double bodyImbalance = (bar.high() - bar.low() > 0)
                        ? ((bar.close() - bar.low()) - (bar.high() - bar.close())) / (bar.high() - bar.low())
                        : 0.0;
                    long buyVol = (long) (bar.volume() * (0.50 + bodyImbalance * 0.35));
                    long sellVol = Math.max(0, bar.volume() - buyVol);
                    m.updateTick(bar.close(), m.getWindowVolume() + bar.volume(), buyVol, sellVol, 0.0, inObsWindow);
                }

                // 2. Active Trade Management (Stop-loss, Breakeven Lock, Trailing Stop, 15:05 Exit)
                if (inTrade) {
                    Map<String, HistoricalMarketDataService.IntradayBar> activeTimeMap = indexedBarsMap.get(activeSym);
                    HistoricalMarketDataService.IntradayBar bar = (activeTimeMap != null) ? activeTimeMap.get(timeStr) : null;
                    if (bar != null) {
                        double tickSize = 0.05;
                        boolean stoppedOut = false;
                        double exitPrice = 0.0;
                        String exitReason = null;

                        if (bar.low() <= activeSl) {
                            stoppedOut = true;
                            exitPrice = round2(activeSl * (1.0 - req.slippagePct() / 100.0));
                            exitReason = activeSl > initialSl ? "TRAILING_SL" : "INITIAL_SL";
                        } else {
                            highWaterMark = Math.max(highWaterMark, bar.high());
                            // Stage 1: Breakeven lock (+1.0% gain)
                            if (highWaterMark >= entryPrice * 1.010) {
                                var beOpt = ChargesBreakdown.calculateExactBreakevenPrice(activeQty, entryPrice, tickSize);
                                if (beOpt.isPresent() && beOpt.get() > activeSl) {
                                    activeSl = beOpt.get();
                                }
                            }
                            // Stage 2: Progressive Trailing (Active ONLY once gain reaches >= 1.5%)
                            if (highWaterMark >= entryPrice * 1.015) {
                                double trailTarget = com.algotrade.algotradebackend.core.util.TickMath.floorToTick(
                                    highWaterMark * (1.0 - req.trailingSlPct() / 100.0), tickSize
                                );
                                // Price Inversion Guard: trailTarget must be strictly below current price
                                if (trailTarget >= activeSl + (3 * tickSize) && trailTarget < bar.close()) {
                                    activeSl = trailTarget;
                                }
                            }
                        }

                        // Mandatory 15:05 Exit
                        if (!stoppedOut && barTime.compareTo(java.time.LocalTime.of(15, 5)) >= 0) {
                            stoppedOut = true;
                            exitPrice = round2(bar.close());
                            exitReason = "MARKET_CLOSE_1505";
                        }

                        if (stoppedOut) {
                            double grossPnL = round2((exitPrice - entryPrice) * activeQty);
                            var charges = ChargesBreakdown.calculate(activeQty, entryPrice, exitPrice);
                            double totalCharges = round2(charges.totalCharges());
                            double netPnL = round2(grossPnL - totalCharges);

                            currentCapital = round2(currentCapital + netPnL);
                            double returnPct = round2((netPnL / (entryPrice * activeQty)) * 100.0);

                            trades.add(new BacktestTrade(
                                tradeCounter++,
                                date.toString(),
                                activeSym,
                                activeScore,
                                entryPrice,
                                exitPrice,
                                activeQty,
                                entryTime,
                                timeStr,
                                exitReason,
                                grossPnL,
                                totalCharges,
                                netPnL,
                                returnPct
                            ));

                            if (netPnL < 0) {
                                dailyLosses++;
                                currentConsecutiveLosses++;
                                maxConsecutiveLosses = Math.max(maxConsecutiveLosses, currentConsecutiveLosses);
                            } else {
                                currentConsecutiveLosses = 0;
                            }

                            dailyTrades++;
                            inTrade = false;
                            activeSym = null;
                        }
                    }
                }

                // 3. Continuous Wave Scanning (09:20 AM to 14:45 PM IST)
                if (!inTrade && dailyTrades < 3 && dailyLosses < 2
                    && barTime.compareTo(java.time.LocalTime.of(9, 20)) >= 0
                    && barTime.compareTo(java.time.LocalTime.of(14, 45)) <= 0) {

                    List<ScoredCandidate> scoredList = metricsMap.values().stream()
                        .filter(m -> !tradedSymbolsToday.contains(m.getSymbol()))
                        .map(m -> scoringEngine.evaluate(m, barTime))
                        .toList();

                    List<ScoredCandidate> ranked = scoringEngine.rankCandidates(scoredList);
                    for (ScoredCandidate candidate : ranked) {
                        if (!candidate.isQualified() || candidate.totalScore() < req.minScoreThreshold()) {
                            break; // Sorted list: subsequent candidates do not meet qualification threshold
                        }

                        CandidateMetrics winnerMetrics = metricsMap.get(candidate.symbol());
                        if (winnerMetrics == null) continue;
                        double rawEntry = candidate.ltp();
                        double effLow = winnerMetrics.getLow15();
                        double tickSize = 0.05;

                        // Structural Support Filter: check support distance <= 1.5%
                        if (effLow <= 0 || effLow >= rawEntry) continue;

                        double supportDistPct = ((rawEntry - effLow) / rawEntry) * 100.0;
                        if (supportDistPct > 1.5) continue;

                        double maxAllowedStop = com.algotrade.algotradebackend.core.util.TickMath.floorToTick(rawEntry * 0.992, tickSize);
                        double calculatedStop = (supportDistPct < 0.25)
                            ? com.algotrade.algotradebackend.core.util.TickMath.floorToTick(rawEntry * 0.9965, tickSize)
                            : com.algotrade.algotradebackend.core.util.TickMath.floorToTick(effLow - tickSize, tickSize);
                        double computedInitialSl = com.algotrade.algotradebackend.core.util.TickMath.floorToTick(Math.max(calculatedStop, maxAllowedStop), tickSize);

                        double slipEntry = round2(rawEntry * (1.0 + req.slippagePct() / 100.0));
                        double stopDistance = slipEntry - computedInitialSl;

                        if (stopDistance <= 0) continue;

                        double stopDistancePct = (stopDistance / slipEntry) * 100.0;
                        if (stopDistancePct < 0.4 || stopDistancePct > 2.0) continue;

                        // 3-Constraint Sizing (Equity-based risk budget and strict leverage ceiling cap)
                        int qRisk = (int) Math.floor((currentCapital * (req.riskPerTradePct() / 100.0)) / stopDistance);
                        int qExposure = (int) Math.floor((currentCapital * req.leverageMultiplier()) / slipEntry);
                        int finalQty = Math.min(qRisk, qExposure);

                        if (finalQty < 1) continue;

                        // Candidate passed all structural and risk sizing gates!
                        inTrade = true;
                        activeSym = candidate.symbol();
                        activeQty = finalQty;
                        entryPrice = slipEntry;
                        initialSl = computedInitialSl;
                        activeSl = computedInitialSl;
                        highWaterMark = slipEntry;
                        entryTime = timeStr;
                        activeScore = candidate.totalScore();
                        tradedSymbolsToday.add(candidate.symbol());
                        break; // Order taken; exit candidate evaluation loop
                    }
                }
            }

            // Daily Equity Curve Point
            double dayNetPnL = round2(currentCapital - dayStartCapital);
            peakCapital = Math.max(peakCapital, currentCapital);
            double currentDdRupees = Math.max(0.0, peakCapital - currentCapital);
            double currentDdPct = peakCapital > 0 ? (currentDdRupees / peakCapital) * 100.0 : 0.0;
            maxDrawdownRupees = Math.max(maxDrawdownRupees, currentDdRupees);
            maxDrawdownPct = Math.max(maxDrawdownPct, currentDdPct);

            double dailyReturn = dayStartCapital > 0 ? (dayNetPnL / dayStartCapital) : 0.0;
            dailyReturns.add(dailyReturn);

            equityCurve.add(new BacktestEquityPoint(
                date.toString(),
                round2(currentCapital),
                0.0,
                dayNetPnL,
                round2(currentDdPct)
            ));
        }

        // Summary Calculations
        int totalTrades = trades.size();
        int winTrades = (int) trades.stream().filter(t -> t.netPnL() > 0).count();
        int lossTrades = (int) trades.stream().filter(t -> t.netPnL() < 0).count();
        double winRatePct = totalTrades > 0 ? round2(((double) winTrades / totalTrades) * 100.0) : 0.0;

        // Institutional Net Profit Factor based strictly on post-tax Net PnL
        double totalNetWins = trades.stream().filter(t -> t.netPnL() > 0).mapToDouble(BacktestTrade::netPnL).sum();
        double totalNetLosses = Math.abs(trades.stream().filter(t -> t.netPnL() < 0).mapToDouble(BacktestTrade::netPnL).sum());
        double profitFactor = totalNetLosses > 0 ? round2(totalNetWins / totalNetLosses) : totalNetWins > 0 ? 99.9 : 0.0;

        double totalGrossPnL = round2(trades.stream().mapToDouble(BacktestTrade::grossPnL).sum());
        double totalTaxes = round2(trades.stream().mapToDouble(BacktestTrade::totalCharges).sum());
        double totalNetPnL = round2(trades.stream().mapToDouble(BacktestTrade::netPnL).sum());
        double totalReturnPct = round2((totalNetPnL / initialCapital) * 100.0);
        double avgTradePnL = totalTrades > 0 ? round2(totalNetPnL / totalTrades) : 0.0;

        // Annualized Sharpe Ratio with Indian benchmark risk-free rate adjustment (6.5%)
        double sharpeRatio = calculateSharpeRatio(dailyReturns);

        BacktestSummary summary = new BacktestSummary(
            round2(initialCapital),
            round2(currentCapital),
            totalGrossPnL,
            totalTaxes,
            totalNetPnL,
            totalReturnPct,
            totalTrades,
            winTrades,
            lossTrades,
            winRatePct,
            profitFactor,
            round2(maxDrawdownPct),
            round2(maxDrawdownRupees),
            maxConsecutiveLosses,
            avgTradePnL,
            round2(sharpeRatio)
        );

        return new BacktestResult(summary, equityCurve, trades);
    }

    private LocalDate parseDate(String dateStr, LocalDate defaultVal) {
        if (dateStr == null || dateStr.isBlank()) return defaultVal;
        try {
            return LocalDate.parse(dateStr.trim());
        } catch (DateTimeParseException e) {
            return defaultVal;
        }
    }

    private double calculateSharpeRatio(List<Double> returns) {
        if (returns.size() < 2) return 0.0;
        double dailyRf = 0.065 / 252.0; // 6.5% Indian annual risk-free rate per trading day
        double mean = returns.stream().mapToDouble(Double::doubleValue).average().orElse(0.0);
        double variance = returns.stream()
            .mapToDouble(r -> Math.pow(r - mean, 2))
            .sum() / (returns.size() - 1);
        double stdDev = Math.sqrt(variance);
        if (stdDev <= 0.00001) return 0.0;
        // Annualize with sqrt(252 trading days)
        return round2(((mean - dailyRf) / stdDev) * Math.sqrt(252));
    }

    private double floorToTick(double price) {
        return Math.floor(price * 20.0) / 20.0;
    }

    private static List<String> generate5mTimeSlots() {
        List<String> slots = new ArrayList<>();
        // 09:15 to 15:30 in 5m increments (75 slots)
        for (int m = 9 * 60 + 15; m <= 15 * 60 + 30; m += 5) {
            slots.add(String.format("%02d:%02d", m / 60, m % 60));
        }
        return slots;
    }

    private static HistoricalMarketDataService.IntradayBar findBarAtTime(List<HistoricalMarketDataService.IntradayBar> bars, String timeStr) {
        if (bars == null || bars.isEmpty()) return null;
        for (HistoricalMarketDataService.IntradayBar b : bars) {
            if (b.time().equals(timeStr)) return b;
        }
        return null;
    }

    private double round2(double val) {
        return Math.round(val * 100.0) / 100.0;
    }
}
