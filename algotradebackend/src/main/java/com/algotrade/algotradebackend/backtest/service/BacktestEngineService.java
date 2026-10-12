package com.algotrade.algotradebackend.backtest.service;

import com.algotrade.algotradebackend.backtest.model.*;
import com.algotrade.algotradebackend.core.model.ChargesBreakdown;
import com.algotrade.algotradebackend.core.model.ScoredCandidate;
import com.algotrade.algotradebackend.strategy.momentum_breakout.CandidateMetrics;
import com.algotrade.algotradebackend.strategy.momentum_breakout.MomentumScoringEngine;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;

import java.time.DayOfWeek;
import java.time.LocalDate;
import java.time.format.DateTimeParseException;
import java.util.*;

/**
 * High-performance backtesting simulation engine for the 15-Min Opening Momentum Breakout Strategy.
 * 
 * <p>Directly reuses production quantitative models:
 * <ul>
 *   <li>{@link MomentumScoringEngine} for 100-point candidate scoring and deterministic tie-breaking.</li>
 *   <li>{@link StatutoryChargesCalculator} for deducting all Indian statutory taxes &amp; charges.</li>
 * </ul>
 * </p>
 */
@Service
public class BacktestEngineService {

    private static final Logger log = LoggerFactory.getLogger(BacktestEngineService.class);

    private final MomentumScoringEngine scoringEngine;
    private final HistoricalMarketDataService dataService;

    public BacktestEngineService(
        MomentumScoringEngine scoringEngine,
        HistoricalMarketDataService dataService
    ) {
        this.scoringEngine = scoringEngine;
        this.dataService = dataService;
    }

    public BacktestResult runBacktest(BacktestRequest req) {
        LocalDate start = parseDate(req.startDate(), LocalDate.now().minusDays(90));
        LocalDate end = parseDate(req.endDate(), LocalDate.now());

        if (start.isAfter(end)) {
            LocalDate tmp = start;
            start = end;
            end = tmp;
        }

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

        for (LocalDate date = start; !date.isAfter(end); date = date.plusDays(1)) {
            // Skip weekend sessions
            if (date.getDayOfWeek() == DayOfWeek.SATURDAY || date.getDayOfWeek() == DayOfWeek.SUNDAY) {
                continue;
            }

            List<CandidateMetrics> metricsList = dataService.getOpeningCandidateMetrics(date);
            List<ScoredCandidate> scoredList = metricsList.stream()
                .map(scoringEngine::evaluate)
                .toList();

            List<ScoredCandidate> ranked = scoringEngine.rankCandidates(scoredList);

            // Filter qualified candidates meeting minimum score threshold
            Optional<ScoredCandidate> topPick = ranked.stream()
                .filter(c -> c.totalScore() >= req.minScoreThreshold())
                .findFirst();

            if (topPick.isEmpty()) {
                // No candidate qualified today
                double ddPct = peakCapital > 0 ? ((peakCapital - currentCapital) / peakCapital) * 100.0 : 0.0;
                equityCurve.add(new BacktestEquityPoint(
                    date.toString(),
                    round2(currentCapital),
                    0.0,
                    0.0,
                    round2(ddPct)
                ));
                dailyReturns.add(0.0);
                continue;
            }

            ScoredCandidate winner = topPick.get();
            CandidateMetrics winnerMetrics = metricsList.stream()
                .filter(m -> m.getSymbol().equals(winner.symbol()))
                .findFirst()
                .orElse(metricsList.get(0));

            // Position Sizing with 0.05% execution slippage
            double rawEntryPrice = winner.ltp();
            double entryPrice = round2(rawEntryPrice * (1.0 + req.slippagePct() / 100.0));
            double initialSl = round2(Math.max(winnerMetrics.getLow15(), entryPrice * 0.985));
            double slDistance = Math.max(0.5, entryPrice - initialSl);

            double riskBudget = currentCapital * (req.riskPerTradePct() / 100.0);
            int quantity = (int) Math.floor(riskBudget / slDistance);

            // Cap with 5x intraday MIS leverage limit
            int maxLeveragedQty = (int) Math.floor((currentCapital * 5.0) / entryPrice);
            quantity = Math.max(1, Math.min(quantity, Math.max(1, maxLeveragedQty)));

            // Intraday 1-minute trajectory replay (09:30 to 15:15)
            List<HistoricalMarketDataService.IntradayBar> bars = dataService.getIntradayTrajectory(
                date, winner.symbol(), entryPrice, initialSl
            );

            double activeSl = initialSl;
            double highWaterMark = entryPrice;
            double exitPrice = entryPrice;
            String exitTime = "15:15";
            String exitReason = "MARKET_CLOSE_1515";

            for (HistoricalMarketDataService.IntradayBar bar : bars) {
                highWaterMark = Math.max(highWaterMark, bar.high());

                // Phase 2: Trailing buffer active only post-11:00 AM IST
                if (bar.time().compareTo("11:00") >= 0) {
                    double trailedSl = highWaterMark * (1.0 - req.trailingSlPct() / 100.0);
                    activeSl = Math.max(activeSl, trailedSl);
                }

                // Check stop loss trigger
                if (bar.low() <= activeSl) {
                    exitPrice = round2(activeSl * (1.0 - req.slippagePct() / 100.0));
                    exitTime = bar.time();
                    exitReason = activeSl > initialSl ? "TRAILING_SL" : "INITIAL_SL";
                    break;
                }
            }

            // Mandatory 15:15 PM exit if SL not triggered
            if ("MARKET_CLOSE_1515".equals(exitReason) && !bars.isEmpty()) {
                exitPrice = round2(bars.get(bars.size() - 1).close());
            }

            // Financial Calculations & Statutory Taxes
            double grossPnL = round2((exitPrice - entryPrice) * quantity);
            var charges = ChargesBreakdown.calculate(quantity, entryPrice, exitPrice);
            double totalCharges = round2(charges.totalCharges());
            double netPnL = round2(grossPnL - totalCharges);

            currentCapital = round2(currentCapital + netPnL);
            double tradeReturnPct = round2((netPnL / (entryPrice * quantity)) * 100.0);

            // Track consecutive losses and drawdowns
            if (netPnL < 0) {
                currentConsecutiveLosses++;
                maxConsecutiveLosses = Math.max(maxConsecutiveLosses, currentConsecutiveLosses);
            } else {
                currentConsecutiveLosses = 0;
            }

            peakCapital = Math.max(peakCapital, currentCapital);
            double currentDdRupees = Math.max(0.0, peakCapital - currentCapital);
            double currentDdPct = peakCapital > 0 ? (currentDdRupees / peakCapital) * 100.0 : 0.0;
            maxDrawdownRupees = Math.max(maxDrawdownRupees, currentDdRupees);
            maxDrawdownPct = Math.max(maxDrawdownPct, currentDdPct);

            dailyReturns.add(netPnL / (currentCapital - netPnL));

            trades.add(new BacktestTrade(
                tradeCounter++,
                date.toString(),
                winner.symbol(),
                winner.totalScore(),
                entryPrice,
                exitPrice,
                quantity,
                "09:30",
                exitTime,
                exitReason,
                grossPnL,
                totalCharges,
                netPnL,
                tradeReturnPct
            ));

            equityCurve.add(new BacktestEquityPoint(
                date.toString(),
                currentCapital,
                grossPnL,
                netPnL,
                round2(currentDdPct)
            ));
        }

        // Summary Calculations
        int totalTrades = trades.size();
        int winTrades = (int) trades.stream().filter(t -> t.netPnL() > 0).count();
        int lossTrades = (int) trades.stream().filter(t -> t.netPnL() < 0).count();
        double winRatePct = totalTrades > 0 ? round2(((double) winTrades / totalTrades) * 100.0) : 0.0;

        double totalGrossWins = trades.stream().filter(t -> t.grossPnL() > 0).mapToDouble(BacktestTrade::grossPnL).sum();
        double totalGrossLosses = Math.abs(trades.stream().filter(t -> t.grossPnL() < 0).mapToDouble(BacktestTrade::grossPnL).sum());
        double profitFactor = totalGrossLosses > 0 ? round2(totalGrossWins / totalGrossLosses) : totalGrossWins > 0 ? 99.9 : 0.0;

        double totalGrossPnL = round2(trades.stream().mapToDouble(BacktestTrade::grossPnL).sum());
        double totalTaxes = round2(trades.stream().mapToDouble(BacktestTrade::totalCharges).sum());
        double totalNetPnL = round2(trades.stream().mapToDouble(BacktestTrade::netPnL).sum());
        double totalReturnPct = round2((totalNetPnL / initialCapital) * 100.0);
        double avgTradePnL = totalTrades > 0 ? round2(totalNetPnL / totalTrades) : 0.0;

        // Annualized Sharpe Ratio approximation
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
        double mean = returns.stream().mapToDouble(Double::doubleValue).average().orElse(0.0);
        double variance = returns.stream()
            .mapToDouble(r -> Math.pow(r - mean, 2))
            .sum() / (returns.size() - 1);
        double stdDev = Math.sqrt(variance);
        if (stdDev <= 0.00001) return 0.0;
        // Annualize with sqrt(252 trading days)
        return (mean / stdDev) * Math.sqrt(252);
    }

    private double round2(double val) {
        return Math.round(val * 100.0) / 100.0;
    }
}
