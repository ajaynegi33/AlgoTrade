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

    @Value("${algotrade.risk.max-daily-trades:1}")
    private int maxDailyTrades;

    private final PaperTradeRepository paperTradeRepository;
    private final java.util.concurrent.atomic.AtomicInteger liveTradesToday = new java.util.concurrent.atomic.AtomicInteger(0);
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

    private void resetLiveTradesIfNewDay() {
        java.time.LocalDate today = java.time.LocalDate.now(java.time.ZoneId.of("Asia/Kolkata"));
        if (lastTradeDate == null || !lastTradeDate.equals(today)) {
            lastTradeDate = today;
            liveTradesToday.set(0);
        }
    }

    /**
     * Determines whether the system is authorized to trade today based on the 1-trade-per-day rule.
     * Enforces circuit-breaker across BOTH live broker dispatches and paper executions.
     * 
     * @return true if trades taken today < maxDailyTrades (1)
     */
    public boolean canTradeToday() {
        resetLiveTradesIfNewDay();
        // Calculate start and end of today's session in IST
        ZoneId istZone = ZoneId.of("Asia/Kolkata");
        Instant startOfDay = LocalDate.now(istZone).atStartOfDay(istZone).toInstant();
        Instant endOfDay = startOfDay.plusSeconds(86400);

        long paperTrades = paperTradeRepository.countByEntryTimeBetween(startOfDay, endOfDay);
        long totalTrades = paperTrades + liveTradesToday.get();
        if (totalTrades >= maxDailyTrades) {
            log.warn("DAILY TRADE LIMIT REACHED: Executed {}/{} trades today (Paper: {}, Live: {}). Standing down.",
                totalTrades, maxDailyTrades, paperTrades, liveTradesToday.get());
            return false;
        }
        return true;
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

        double stopDistance = entryPrice - stopPrice;
        if (stopDistance <= 0) {
            log.warn("INVALID STOP: Stop price (₹{}) must be strictly below entry price (₹{})", stopPrice, entryPrice);
            return 0;
        }

        double stopDistancePct = (stopDistance / entryPrice) * 100.0;

        // Gate 1: Min Stop Distance Check (Noise Guard)
        if (stopDistancePct < minStopDistancePct) {
            log.warn("REJECTED: Stop distance ({:.2f}%) too narrow (< {:.2f}%). Risk of market noise stop-out.",
                stopDistancePct, minStopDistancePct);
            return 0;
        }

        // Gate 2: Max Stop Distance Check (Budget Protection)
        if (stopDistancePct > maxStopDistancePct) {
            log.warn("REJECTED: Stop distance ({:.2f}%) too wide (> {:.2f}%). Exceeds planned risk tolerance.",
                stopDistancePct, maxStopDistancePct);
            return 0;
        }

        // Constraint 1: Risk-Based Quantity (Q_risk)
        // Planned Risk Budget in INR = Capital * Risk% (e.g. ₹10,000 * 1% = ₹100)
        double plannedRiskBudget = capital * (riskBudgetPct / 100.0);
        int qRisk = (int) Math.floor(plannedRiskBudget / stopDistance);

        // Constraint 2: Available Margin Quantity (Q_margin)
        double availableCash = gateway.getAvailableCash();
        double marginPerShare = gateway.calculateOrderMargin(symbol, 1, entryPrice);
        int qMargin = marginPerShare > 0 ? (int) Math.floor(availableCash / marginPerShare) : 0;

        // Constraint 3: Maximum Exposure Quantity (Q_exposure)
        // Caps total position value to Authorized Exposure (e.g. ₹10,000 * 1.0x = ₹10,000)
        double maxPositionValue = capital * maxExposureMultiplier;
        int qExposure = (int) Math.floor(maxPositionValue / entryPrice);

        // Final Sizing Resolution: Smallest of the 3 constraints
        int finalQuantity = Math.min(qRisk, Math.min(qMargin, qExposure));

        log.info("SIZING RESOLUTION for {}: Q_risk={}, Q_margin={}, Q_exposure={} -> FINAL QUANTITY = {} shares (Total Value: ₹{})",
            symbol, qRisk, qMargin, qExposure, finalQuantity, Math.round(finalQuantity * entryPrice));

        return finalQuantity;
    }

    public double getCapital() { return capital; }
    public double getRiskBudget() { return capital * (riskBudgetPct / 100.0); }
}
