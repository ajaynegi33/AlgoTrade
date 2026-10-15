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

    @Value("${algotrade.risk.min-stop-distance-pct:0.35}")
    private double minStopDistancePct;

    @Value("${algotrade.risk.max-stop-distance-pct:2.0}")
    private double maxStopDistancePct;

    @Value("${algotrade.risk.max-daily-trades:3}")
    private int maxDailyTrades;

    @Value("${algotrade.risk.max-daily-losses:2}")
    private int maxDailyLosses;

    private final PaperTradeRepository paperTradeRepository;

    @org.springframework.beans.factory.annotation.Autowired(required = false)
    private org.springframework.beans.factory.ObjectProvider<com.algotrade.algotradebackend.broker.BrokerGatewayManager> brokerGatewayManagerProvider;

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

        // Risk budget applied strictly to actual account equity (e.g. ₹10,000 * 2.0% = ₹200 risk capacity)
        double plannedRiskBudget = accountCapital * (riskBudgetPct / 100.0);
        int qRisk = (int) Math.floor(plannedRiskBudget / stopDistance);

        // Maximum leveraged exposure ceiling (e.g. ₹10,000 * 5x = ₹50,000 max position value)
        double maxPositionValue = accountCapital * maxExposureMultiplier;
        int qExposure = (int) Math.floor(maxPositionValue / entryPrice);

        return Math.max(0, Math.min(qRisk, qExposure));
    }

    /**
     * Validates a manually submitted order against daily trade limits, available margin,
     * maximum exposure ceiling, and maximum rupee risk.
     *
     * @param symbol Trading symbol (e.g. "RELIANCE")
     * @param customQuantity User-specified quantity
     * @param entryPrice Estimated or requested entry price
     * @param stopPrice Intended stop-loss price
     * @param gateway Active broker gateway
     * @throws IllegalStateException if daily limits or loss circuit breakers are active
     * @throws IllegalArgumentException if quantity, price, margin, or risk constraints are violated
     */
    public void validateManualOrder(
        String symbol,
        int customQuantity,
        double entryPrice,
        double stopPrice,
        BrokerGateway gateway
    ) {
        if (!canTradeToday()) {
            throw new IllegalStateException("Trading halted: Daily trade limit or loss circuit breaker reached.");
        }
        if (customQuantity <= 0) {
            throw new IllegalArgumentException("Custom quantity must be strictly positive, got: " + customQuantity);
        }
        if (entryPrice <= 0 || stopPrice <= 0 || stopPrice >= entryPrice) {
            throw new IllegalArgumentException("Invalid price parameters: Stop price must be strictly below entry price.");
        }
        double stopDistPct = ((entryPrice - stopPrice) / entryPrice) * 100.0;
        if (stopDistPct < minStopDistancePct || stopDistPct > maxStopDistancePct) {
            throw new IllegalArgumentException(String.format(
                "Stop distance %.2f%% outside allowable range [%.2f%%, %.2f%%]",
                stopDistPct, minStopDistancePct, maxStopDistancePct
            ));
        }
        double activeCapital = (gateway != null) ? gateway.getTotalCapital() : getCapital();
        double availableCash = (gateway != null) ? gateway.getAvailableCash() : activeCapital;
        double marginPerShare = (gateway != null) ? gateway.calculateOrderMargin(symbol, 1, entryPrice) : (entryPrice / maxExposureMultiplier);
        if (marginPerShare <= 0) {
            marginPerShare = entryPrice / maxExposureMultiplier;
        }

        // 1. Margin cash capacity check
        double totalRequiredMargin = customQuantity * marginPerShare;
        if (totalRequiredMargin > availableCash) {
            throw new IllegalArgumentException(String.format(
                "Custom quantity %d requires ₹%.2f margin, exceeding available cash ₹%.2f",
                customQuantity, totalRequiredMargin, availableCash
            ));
        }

        // 2. Maximum leveraged exposure ceiling check (with 5% buffer for tick precision)
        double maxAllowedExposure = activeCapital * maxExposureMultiplier;
        double orderExposure = customQuantity * entryPrice;
        if (orderExposure > maxAllowedExposure * 1.05) {
            throw new IllegalArgumentException(String.format(
                "Custom quantity %d creates exposure ₹%.2f, exceeding maximum permitted ₹%.2f (%.1fx leverage)",
                customQuantity, orderExposure, maxAllowedExposure, maxExposureMultiplier
            ));
        }

        // 3. Absolute equity risk ceiling check (1.5x risk tolerance for manual discretionary entries)
        double rupeeRisk = customQuantity * (entryPrice - stopPrice);
        double maxAllowedRisk = activeCapital * (riskBudgetPct / 100.0) * 1.50;
        if (rupeeRisk > maxAllowedRisk) {
            throw new IllegalArgumentException(String.format(
                "Custom quantity %d risks ₹%.2f, exceeding risk budget cap ₹%.2f",
                customQuantity, rupeeRisk, maxAllowedRisk
            ));
        }
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

        double activeCapital = (gateway != null) ? gateway.getTotalCapital() : getCapital();
        int baseQty = calculateSizingQuantity(activeCapital, entryPrice, stopPrice, riskBudgetPct, maxExposureMultiplier, minStopDistancePct, maxStopDistancePct);
        if (baseQty <= 0) {
            double stopDistance = entryPrice - stopPrice;
            if (stopDistance <= 0) {
                log.warn("INVALID STOP: Stop price (₹{}) must be strictly below entry price (₹{})", stopPrice, entryPrice);
            } else {
                double stopDistancePct = (stopDistance / entryPrice) * 100.0;
                log.warn("REJECTED: Stop distance ({}%) outside allowed range [{}%, {}%]",
                    String.format("%.2f", stopDistancePct), minStopDistancePct, maxStopDistancePct);
            }
            return 0;
        }

        // Constraint 2: Available Margin Quantity (Q_margin)
        double availableCash = gateway != null ? gateway.getAvailableCash() : activeCapital;
        double marginPerShare = gateway != null ? gateway.calculateOrderMargin(symbol, 1, entryPrice) : (entryPrice / maxExposureMultiplier);
        if (marginPerShare <= 0) {
            marginPerShare = entryPrice / maxExposureMultiplier;
        }

        int qMargin = (int) Math.floor(availableCash / marginPerShare);

        // Constraint 3: Target Exposure Ceiling (Q_exposure = activeCapital * maxExposureMultiplier / entryPrice)
        double maxExposure = activeCapital * maxExposureMultiplier;
        int qExposure = (int) Math.floor(maxExposure / entryPrice);

        // Final Sizing Resolution: Smallest of base risk quantity, available margin, and exposure ceiling
        int finalQuantity = Math.min(baseQty, Math.min(qMargin, qExposure));

        log.info("SIZING RESOLUTION for {}: ActiveCapital=₹{}, BaseQty={}, Q_margin={}, Q_exposure={} -> FINAL QUANTITY = {} shares (Total Value: ₹{})",
            symbol, activeCapital, baseQty, qMargin, qExposure, finalQuantity, Math.round(finalQuantity * entryPrice));

        return finalQuantity;
    }

    public double getCapital() {
        if (brokerGatewayManagerProvider != null) {
            var manager = brokerGatewayManagerProvider.getIfAvailable();
            if (manager != null && manager.getActiveGateway() != null) {
                return manager.getActiveGateway().getTotalCapital();
            }
        }
        return capital;
    }

    public double getBaseCapital() {
        if (brokerGatewayManagerProvider != null) {
            var manager = brokerGatewayManagerProvider.getIfAvailable();
            if (manager != null && manager.getActiveGateway() != null) {
                return manager.getActiveGateway().getBaseCapital();
            }
        }
        return capital;
    }

    public double getCumulativeNetPnL() {
        if (brokerGatewayManagerProvider != null) {
            var manager = brokerGatewayManagerProvider.getIfAvailable();
            if (manager != null && manager.getActiveGateway() != null) {
                return manager.getActiveGateway().getCumulativeNetPnL();
            }
        }
        return 0.0;
    }

    public double getRiskBudget() {
        return Math.round(getCapital() * (riskBudgetPct / 100.0) * 100.0) / 100.0;
    }
}
