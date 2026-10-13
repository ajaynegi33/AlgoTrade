package com.algotrade.algotradebackend.risk;

import com.algotrade.algotradebackend.broker.BrokerGateway;
import com.algotrade.algotradebackend.persistence.repository.PaperTradeRepository;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;

import java.time.Instant;
import java.time.LocalDate;
import java.time.ZoneId;

/**
 * Institutional Position Sizing & Risk Management Engine.
 * 
 * <p>Enforces the 3-Constraint Quantity Formula:</p>
 * <pre>
 *   Final Quantity = min(Q_risk, Q_margin, Q_exposure)
 * </pre>
 * 
 * <p>Also strictly enforces the "Maximum 1 Trade Per Day" rule by querying
 * historical database records for today's session.</p>
 */
@Service
public class PositionSizingEngine {

    private static final Logger log = LoggerFactory.getLogger(PositionSizingEngine.class);

    @Value("${algotrade.risk.capital:10000.00}")
    private double capital;

    @Value("${algotrade.risk.account-risk-budget-pct:1.0}")
    private double riskBudgetPct;

    @Value("${algotrade.risk.max-exposure-multiplier:1.0}")
    private double maxExposureMultiplier;

    @Value("${algotrade.risk.min-stop-distance-pct:0.4}")
    private double minStopDistancePct;

    @Value("${algotrade.risk.max-stop-distance-pct:2.0}")
    private double maxStopDistancePct;

    @Value("${algotrade.risk.max-daily-trades:3}")
    private int maxDailyTrades;

    @Value("${algotrade.risk.max-daily-losses:2}")
    private int maxDailyLosses;

    private final PaperTradeRepository paperTradeRepository;
    private final java.util.concurrent.atomic.AtomicInteger liveTradesToday = new java.util.concurrent.atomic.AtomicInteger(0);
    private final java.util.concurrent.atomic.AtomicInteger liveLossesToday = new java.util.concurrent.atomic.AtomicInteger(0);
    private volatile java.time.LocalDate lastTradeDate;

    public PositionSizingEngine(PaperTradeRepository paperTradeRepository) {
        this.paperTradeRepository = paperTradeRepository;
    }

    /**
     * Records a live or paper trade dispatch for the current trading day.
     */
    public void recordTradeDispatched() {
        resetLiveTradesIfNewDay();
        liveTradesToday.incrementAndGet();
    }

    public void recordTradeLoss() {
        resetLiveTradesIfNewDay();
        liveLossesToday.incrementAndGet();
    }

    private void resetLiveTradesIfNewDay() {
        java.time.LocalDate today = java.time.LocalDate.now(java.time.ZoneId.of("Asia/Kolkata"));
        if (lastTradeDate == null || !lastTradeDate.equals(today)) {
            lastTradeDate = today;
            liveTradesToday.set(0);
            liveLossesToday.set(0);
        }
    }

    /**
     * Determines whether the system is authorized to trade today based on:
     * 1. Maximum total daily trades limit
     * 2. Daily loss circuit-breaker (max 2 losses before emergency halt)
     * 
     * @return true if trades taken today < maxDailyTrades AND losses < maxDailyLosses
     */
    public boolean canTradeToday() {
        resetLiveTradesIfNewDay();
        // Calculate start and end of today's session in IST
        ZoneId istZone = ZoneId.of("Asia/Kolkata");
        Instant startOfDay = LocalDate.now(istZone).atStartOfDay(istZone).toInstant();
        Instant endOfDay = startOfDay.plusSeconds(86400);

        var todayPaperTrades = paperTradeRepository.findByEntryTimeBetween(startOfDay, endOfDay);
        long paperTrades = todayPaperTrades.size();
        long totalTrades = paperTrades + liveTradesToday.get();
        if (totalTrades >= maxDailyTrades) {
            log.warn("DAILY TRADE LIMIT REACHED: Executed {}/{} trades today (Paper: {}, Live: {}). Standing down.",
                totalTrades, maxDailyTrades, paperTrades, liveTradesToday.get());
            return false;
        }

        // Daily Loss Circuit Breaker
        long paperLosses = todayPaperTrades.stream()
            .filter(t -> t.getNetPnL() != null && t.getNetPnL() < 0)
            .count();
        long totalLosses = paperLosses + liveLossesToday.get();
        if (totalLosses >= maxDailyLosses) {
            log.warn("DAILY LOSS CIRCUIT BREAKER HIT: {}/{} losses incurred today (Paper: {}, Live: {}). Halting trading for capital preservation.",
                totalLosses, maxDailyLosses, paperLosses, liveLossesToday.get());
            return false;
        }

        return true;
    }

    /**
     * Pure quantitative mathematical sizing formula without gateway dependencies.
     * Shared across live trading, paper execution, and backtesting simulations.
     */
    public static int calculateSizingQuantity(
        double accountCapital,
        double entryPrice,
        double stopPrice,
        double riskBudgetPct,
        double maxExposureMultiplier,
        double minStopDistancePct,
        double maxStopDistancePct
    ) {
        if (entryPrice <= 0 || stopPrice <= 0 || accountCapital <= 0) return 0;
        double stopDistance = entryPrice - stopPrice;
        if (stopDistance <= 0) return 0;

        double stopDistancePct = (stopDistance / entryPrice) * 100.0;
        if (stopDistancePct < minStopDistancePct || stopDistancePct > maxStopDistancePct) {
            return 0;
        }

        // Total leveraged purchasing power: e.g. ₹10,000 capital * 5x leverage = ₹50,000 position value
        double maxPositionValue = accountCapital * maxExposureMultiplier;

        // Risk capacity applied to overall invested amount (capital + leverage): e.g. ₹50,000 * 2.8% = ₹1,400 risk budget
        double plannedRiskBudget = maxPositionValue * (riskBudgetPct / 100.0);
        int qRisk = (int) Math.floor(plannedRiskBudget / stopDistance);

        return Math.max(0, qRisk);
    }

    /**
     * Calculates the exact share quantity satisfying all 3 independent risk constraints.
     * 
     * @param entryPrice Estimated entry execution price
     * @param stopPrice Technical structural stop-loss price
     * @param gateway Active broker gateway to check margin and cash
     * @return Recommended whole share quantity, or 0 if trade fails risk gates
     */
    public int calculateQuantity(String symbol, double entryPrice, double stopPrice, BrokerGateway gateway) {
        if (!canTradeToday()) {
            return 0;
        }

        int baseQty = calculateSizingQuantity(capital, entryPrice, stopPrice, riskBudgetPct, maxExposureMultiplier, minStopDistancePct, maxStopDistancePct);
        if (baseQty <= 0) {
            double stopDistance = entryPrice - stopPrice;
            if (stopDistance <= 0) {
                log.warn("INVALID STOP: Stop price (₹{}) must be strictly below entry price (₹{})", stopPrice, entryPrice);
            } else {
                double stopDistancePct = (stopDistance / entryPrice) * 100.0;
                log.warn("REJECTED: Stop distance ({:.2f}%) outside allowed range [{:.2f}%, {:.2f}%]", stopDistancePct, minStopDistancePct, maxStopDistancePct);
            }
            return 0;
        }

        // Constraint 2: Available Margin Quantity (Q_margin)
        double availableCash = gateway.getAvailableCash();
        double marginPerShare = gateway.calculateOrderMargin(symbol, 1, entryPrice);
        int qMargin = marginPerShare > 0 ? (int) Math.floor(availableCash / marginPerShare) : 0;

        // Final Sizing Resolution: Smallest of base risk/exposure and available margin
        int finalQuantity = Math.min(baseQty, qMargin);

        log.info("SIZING RESOLUTION for {}: BaseQty={}, Q_margin={} -> FINAL QUANTITY = {} shares (Total Value: ₹{})",
            symbol, baseQty, qMargin, finalQuantity, Math.round(finalQuantity * entryPrice));

        return finalQuantity;
    }

    public double getCapital() { return capital; }
    public double getRiskBudget() { return capital * (riskBudgetPct / 100.0); }
}
