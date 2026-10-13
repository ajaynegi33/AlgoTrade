package com.algotrade.algotradebackend.strategy;

import com.algotrade.algotradebackend.broker.BrokerGatewayManager;
import com.algotrade.algotradebackend.core.model.ScoredCandidate;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;

import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Strategy Manager implementing the Strategy Pattern registry.
 * 
 * <p>Dispatches real-time ticks and schedule triggers to the currently active strategy.
 * Allows hot-swapping strategies dynamically without touching core execution code.</p>
 */
@Service
public class StrategyManager {

    private static final Logger log = LoggerFactory.getLogger(StrategyManager.class);

    private final Map<String, TradingStrategy> strategies = new ConcurrentHashMap<>();
    private final BrokerGatewayManager brokerGatewayManager;
    private volatile String activeStrategyId;

    public StrategyManager(List<TradingStrategy> strategyList, BrokerGatewayManager brokerGatewayManager) {
        this.brokerGatewayManager = brokerGatewayManager;
        for (TradingStrategy strategy : strategyList) {
            strategies.put(strategy.getStrategyId(), strategy);
            log.info("Registered Trading Strategy: [{}] - {}", strategy.getStrategyId(), strategy.getStrategyName());
        }

        // Set default strategy to MOMENTUM_BREAKOUT if present, otherwise first available
        if (strategies.containsKey("MOMENTUM_BREAKOUT")) {
            this.activeStrategyId = "MOMENTUM_BREAKOUT";
        } else if (!strategyList.isEmpty()) {
            this.activeStrategyId = strategyList.get(0).getStrategyId();
        }
        log.info("Active Strategy set to: [{}]", activeStrategyId);
    }

    public TradingStrategy getActiveStrategy() {
        return strategies.get(activeStrategyId);
    }

    public synchronized void setActiveStrategy(String strategyId) {
        if (!strategies.containsKey(strategyId)) {
            throw new IllegalArgumentException("Strategy not found: " + strategyId);
        }
        this.activeStrategyId = strategyId;
        log.warn("=== ACTIVE STRATEGY SWITCHED TO: [{}] ===", strategyId);
    }

    public void onTick(String symbol, double ltp, long volume, long totalBuyQty, long totalSellQty) {
        TradingStrategy strategy = getActiveStrategy();
        if (strategy != null) {
            strategy.onTick(symbol, ltp, volume, totalBuyQty, totalSellQty);
        }
        if (brokerGatewayManager != null && brokerGatewayManager.getPaperTradingGateway() != null) {
            brokerGatewayManager.getPaperTradingGateway().onTick(symbol, ltp);
        }
    }

    public void evaluate0930() {
        TradingStrategy strategy = getActiveStrategy();
        if (strategy != null) {
            strategy.evaluate0930();
        }
    }

    public void executeSquareOff() {
        TradingStrategy strategy = getActiveStrategy();
        if (strategy != null) {
            strategy.executeSquareOff();
        }
    }

    public void executeSquareOff1515() {
        executeSquareOff();
    }

    public List<ScoredCandidate> getLatestTopCandidates() {
        TradingStrategy strategy = getActiveStrategy();
        return strategy != null ? strategy.getLatestTopCandidates() : List.of();
    }

    public java.util.Map<String, Object> executeManualEntry(String symbol, Integer quantity, Double customStopPrice) {
        return executeManualEntry(symbol, quantity, customStopPrice, null);
    }

    public java.util.Map<String, Object> executeManualEntry(String symbol, Integer quantity, Double customStopPrice, Double entryPrice) {
        TradingStrategy strategy = getActiveStrategy();
        if (strategy == null) {
            throw new IllegalStateException("No active trading strategy is currently configured.");
        }
        return strategy.executeManualEntry(symbol, quantity, customStopPrice, entryPrice);
    }
}
