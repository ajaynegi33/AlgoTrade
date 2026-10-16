package com.algotrade.algotradebackend.risk;

import com.algotrade.algotradebackend.broker.BrokerGateway;
import com.algotrade.algotradebackend.core.model.ChargesBreakdown;
import com.algotrade.algotradebackend.core.model.ExecutionMode;
import com.algotrade.algotradebackend.persistence.repository.PaperTradeRepository;
import com.algotrade.algotradebackend.persistence.repository.LiveTradeRepository;
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

    @Value("${algotrade.risk.max-daily-loss-rupees:400.00}")
    private double maxDailyLossRupees;

    private final PaperTradeRepository paperTradeRepository;

    @org.springframework.beans.factory.annotation.Autowired(required = false)
    private LiveTradeRepository liveTradeRepository;

    @org.springframework.beans.factory.annotation.Autowired(required = false)
    private org.springframework.beans.factory.ObjectProvider<com.algotrade.algotradebackend.broker.BrokerGatewayManager> brokerGatewayManagerProvider;

    private final java.util.concurrent.atomic.AtomicInteger liveTradesToday = new java.util.concurrent.atomic.AtomicInteger(0);
    private final java.util.concurrent.atomic.AtomicInteger liveLossesToday = new java.util.concurrent.atomic.AtomicInteger(0);
    private final java.util.concurrent.atomic.AtomicReference<Double> liveRupeeLossToday = new java.util.concurrent.atomic.AtomicReference<>(0.0);
    private volatile java.time.LocalDate lastTradeDate;

    // High-frequency tick caching and log debouncing
    private final java.util.concurrent.atomic.AtomicBoolean circuitBreakerLogged = new java.util.concurrent.atomic.AtomicBoolean(false);
    private final java.util.concurrent.atomic.AtomicBoolean dailyLimitLogged = new java.util.concurrent.atomic.AtomicBoolean(false);
    private volatile long lastWarningLogMs = 0L;
    private volatile boolean cachedCanTrade = true;
    private volatile long lastCanTradeCheckMs = 0L;
    private static final long CAN_TRADE_CACHE_TTL_MS = 2000L; // 2 seconds cache to protect SQLite on ReadingThread
    private volatile Instant paperCircuitResetInstant = null;

    public PositionSizingEngine(PaperTradeRepository paperTradeRepository) {
        this(paperTradeRepository, null);
    }

    @org.springframework.beans.factory.annotation.Autowired
    public PositionSizingEngine(PaperTradeRepository paperTradeRepository, @org.springframework.beans.factory.annotation.Autowired(required = false) LiveTradeRepository liveTradeRepository) {
        this.paperTradeRepository = paperTradeRepository;
        this.liveTradeRepository = liveTradeRepository;
    }

    private ExecutionMode getActiveExecutionMode() {
        if (brokerGatewayManagerProvider != null) {
            var manager = brokerGatewayManagerProvider.getIfAvailable();
            if (manager != null) {
                return manager.getActiveMode();
            }
        }
        return ExecutionMode.PAPER;
    }

    /**
     * Records a live or paper trade dispatch for the current trading day.
     */
    public void recordTradeDispatched() {
        recordTradeDispatched(getActiveExecutionMode());
    }

    public void recordTradeDispatched(ExecutionMode mode) {
        resetLiveTradesIfNewDay();
        if (mode == ExecutionMode.LIVE) {
            liveTradesToday.incrementAndGet();
        }
        lastCanTradeCheckMs = 0L; // Invalidate cache immediately on new order dispatch
    }

    public void recordTradeLoss() {
        recordTradeLoss(getActiveExecutionMode(), 0.0);
    }

    public void recordTradeLoss(ExecutionMode mode) {
        recordTradeLoss(mode, 0.0);
    }

    public void recordTradeLoss(ExecutionMode mode, double netLossRupees) {
        resetLiveTradesIfNewDay();
        if (mode == ExecutionMode.LIVE) {
            liveLossesToday.incrementAndGet();
            if (netLossRupees > 0) {
                liveRupeeLossToday.updateAndGet(curr -> curr + netLossRupees);
            }
        }
        lastCanTradeCheckMs = 0L; // Invalidate cache immediately on trade loss
    }

    /**
     * Manually resets daily trade limits and loss circuit breakers.
     * Useful for paper testing and resuming operations without database wipe.
     */
    public void resetCircuitBreaker() {
        resetLiveTradesIfNewDay();
        liveTradesToday.set(0);
        liveLossesToday.set(0);
        liveRupeeLossToday.set(0.0);
        circuitBreakerLogged.set(false);
        dailyLimitLogged.set(false);
        paperCircuitResetInstant = Instant.now();
        lastCanTradeCheckMs = 0L;
        cachedCanTrade = true;
        log.info("Risk engine daily trade limits and loss circuit breakers have been manually reset.");
    }

    private void resetLiveTradesIfNewDay() {
        java.time.LocalDate today = java.time.LocalDate.now(java.time.ZoneId.of("Asia/Kolkata"));
        if (lastTradeDate == null || !lastTradeDate.equals(today)) {
            lastTradeDate = today;
            liveTradesToday.set(0);
            liveLossesToday.set(0);
            liveRupeeLossToday.set(0.0);
            circuitBreakerLogged.set(false);
            dailyLimitLogged.set(false);
            paperCircuitResetInstant = null;
            lastCanTradeCheckMs = 0L;
        }
    }

    /**
     * Determines whether the system is authorized to trade today based on:
     * 1. Maximum total daily trades limit
     * 2. Daily loss count circuit-breaker (max 2 losses before emergency halt)
     * 3. Cumulative daily rupee drawdown limit (max ₹400 loss on ₹10k capital)
     * 
     * @return true if trades taken today < maxDailyTrades AND losses < maxDailyLosses AND rupeeLoss < maxDailyLossRupees
     */
    public boolean canTradeToday() {
        long now = System.currentTimeMillis();
        if (now - lastCanTradeCheckMs < CAN_TRADE_CACHE_TTL_MS) {
            return cachedCanTrade;
        }

        resetLiveTradesIfNewDay();
        // Calculate start and end of today's session in IST
        ZoneId istZone = ZoneId.of("Asia/Kolkata");
        Instant startOfDay = LocalDate.now(istZone).atStartOfDay(istZone).toInstant();
        Instant endOfDay = startOfDay.plusSeconds(86400);

        Instant queryStart = (paperCircuitResetInstant != null && paperCircuitResetInstant.isAfter(startOfDay))
            ? paperCircuitResetInstant : startOfDay;

        var todayPaperTrades = paperTradeRepository.findByEntryTimeBetween(queryStart, endOfDay);
        long paperTrades = (todayPaperTrades != null) ? todayPaperTrades.size() : 0;
        long paperLosses = (todayPaperTrades != null)
            ? todayPaperTrades.stream().filter(t -> t.getNetPnL() != null && t.getNetPnL() < 0).count()
            : 0;
        double paperRupeeLoss = (todayPaperTrades != null)
            ? todayPaperTrades.stream()
                .filter(t -> t.getNetPnL() != null && t.getNetPnL() < 0)
                .mapToDouble(t -> Math.abs(t.getNetPnL()))
                .sum()
            : 0.0;

        ExecutionMode activeMode = getActiveExecutionMode();
        long effectiveTrades;
        long effectiveLosses;
        double effectiveRupeeLoss;

        if (activeMode == ExecutionMode.LIVE) {
            if (liveTradeRepository != null) {
                var todayLiveTrades = liveTradeRepository.findByEntryTimeBetween(queryStart, endOfDay);
                long dbLiveTrades = (todayLiveTrades != null) ? todayLiveTrades.size() : 0;
                long dbLiveLosses = (todayLiveTrades != null)
                    ? todayLiveTrades.stream().filter(t -> t.getNetPnL() != null && t.getNetPnL() < 0).count()
                    : 0;
                double dbLiveRupeeLoss = (todayLiveTrades != null)
                    ? todayLiveTrades.stream()
                        .filter(t -> t.getNetPnL() != null && t.getNetPnL() < 0)
                        .mapToDouble(t -> Math.abs(t.getNetPnL()))
                        .sum()
                    : 0.0;
                effectiveTrades = Math.max(liveTradesToday.get(), dbLiveTrades);
                effectiveLosses = Math.max(liveLossesToday.get(), dbLiveLosses);
                effectiveRupeeLoss = Math.max(liveRupeeLossToday.get(), dbLiveRupeeLoss);
            } else {
                effectiveTrades = liveTradesToday.get();
                effectiveLosses = liveLossesToday.get();
                effectiveRupeeLoss = liveRupeeLossToday.get();
            }
        } else {
            effectiveTrades = paperTrades;
            effectiveLosses = paperLosses;
            effectiveRupeeLoss = paperRupeeLoss;
        }

        if (effectiveTrades >= maxDailyTrades) {
            if (!dailyLimitLogged.getAndSet(true) || (now - lastWarningLogMs > 300_000L)) {
                log.warn("DAILY TRADE LIMIT REACHED [{} Mode]: Executed {}/{} trades today (Paper: {}, Live: {}). Standing down.",
                    activeMode, effectiveTrades, maxDailyTrades, paperTrades, liveTradesToday.get());
                lastWarningLogMs = now;
            }
            cachedCanTrade = false;
            lastCanTradeCheckMs = now;
            return false;
        }
        dailyLimitLogged.set(false);

        // Daily Loss Circuit Breaker (Count Limit or Rupee Drawdown Limit)
        if (effectiveLosses >= maxDailyLosses || (maxDailyLossRupees > 0 && effectiveRupeeLoss >= maxDailyLossRupees)) {
            if (!circuitBreakerLogged.getAndSet(true) || (now - lastWarningLogMs > 300_000L)) {
                log.warn("DAILY LOSS CIRCUIT BREAKER HIT [{} Mode]: {}/{} losses incurred (₹{}/₹{} drawdown today). Halting trading for capital preservation.",
                    activeMode, effectiveLosses, maxDailyLosses, Math.round(effectiveRupeeLoss * 100.0) / 100.0, maxDailyLossRupees);
                lastWarningLogMs = now;
            }
            cachedCanTrade = false;
            lastCanTradeCheckMs = now;
            return false;
        }
        circuitBreakerLogged.set(false);

        cachedCanTrade = true;
        lastCanTradeCheckMs = now;
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
     * Institutional all-inclusive sizing formula factoring in:
     * 1. Direct price distance (entry - stop)
     * 2. Exit slippage buffer (e.g. 0.15% limit offset)
     * 3. Statutory Indian equity transaction costs (STT, brokerage, exchange turnover, SEBI, GST, stamp duty)
     * 4. Strict exposure leverage ceiling cap
     */
    public static int calculateAllInclusiveSizingQuantity(
        double accountCapital,
        double entryPrice,
        double stopPrice,
        double riskBudgetPct,
        double maxExposureMultiplier,
        double minStopDistancePct,
        double maxStopDistancePct,
        double slippagePct
    ) {
        if (entryPrice <= 0 || stopPrice <= 0 || accountCapital <= 0 || entryPrice <= stopPrice) return 0;
        double stopDistance = entryPrice - stopPrice;
        double stopDistancePct = (stopDistance / entryPrice) * 100.0;
        if (stopDistancePct < minStopDistancePct || stopDistancePct > maxStopDistancePct) {
            return 0;
        }

        double plannedRiskBudget = accountCapital * (riskBudgetPct / 100.0);
        double maxPositionValue = accountCapital * maxExposureMultiplier;
        int qExposure = (int) Math.floor(maxPositionValue / entryPrice);

        double expectedSlippagePerShare = stopPrice * (Math.max(0.0, slippagePct) / 100.0);
        double grossRiskPerShare = stopDistance + expectedSlippagePerShare;
        if (grossRiskPerShare <= 0) return 0;

        int candidateQty = (int) Math.floor(plannedRiskBudget / grossRiskPerShare);
        candidateQty = Math.min(candidateQty, qExposure);

        // Fail-closed verification loop factoring in statutory Indian equity taxes, brokerage, and slippage
        while (candidateQty > 0) {
            double exitPriceWithSlippage = stopPrice - expectedSlippagePerShare;
            ChargesBreakdown charges = ChargesBreakdown.calculate(candidateQty, entryPrice, exitPriceWithSlippage);
            double totalLoss = Math.abs(charges.netPnL());
            if (totalLoss <= plannedRiskBudget) {
                break;
            }
            candidateQty--;
        }

        return Math.max(0, candidateQty);
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
        int baseQty = calculateAllInclusiveSizingQuantity(
            activeCapital,
            entryPrice,
            stopPrice,
            riskBudgetPct,
            maxExposureMultiplier,
            minStopDistancePct,
            maxStopDistancePct,
            0.15
        );
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
