package com.algotrade.algotradebackend.broker;

import com.algotrade.algotradebackend.broker.paper.PaperTradingGateway;
import com.algotrade.algotradebackend.broker.zerodha.ZerodhaLiveGateway;
import com.algotrade.algotradebackend.core.model.ExecutionMode;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;

import java.util.concurrent.atomic.AtomicReference;

/**
 * Hot-swappable Broker Manager.
 * 
 * <p>Implements the Gateway/Adapter Pattern by routing all execution requests
 * to the currently active BrokerGateway (PaperTradingGateway vs ZerodhaLiveGateway).</p>
 * 
 * <p>Enables safe runtime switching via REST API from the Next.js frontend toggle.</p>
 */
@Service
public class BrokerGatewayManager {

    private static final Logger log = LoggerFactory.getLogger(BrokerGatewayManager.class);

    private final PaperTradingGateway paperTradingGateway;
    private final ZerodhaLiveGateway zerodhaLiveGateway;
    private final AtomicReference<ExecutionMode> activeMode;

    @org.springframework.beans.factory.annotation.Autowired(required = false)
    private org.springframework.beans.factory.ObjectProvider<com.algotrade.algotradebackend.strategy.momentum_breakout.MomentumBreakoutStrategy> strategyProvider;

    public BrokerGatewayManager(
        PaperTradingGateway paperTradingGateway,
        ZerodhaLiveGateway zerodhaLiveGateway,
        @Value("${algotrade.mode:PAPER}") String initialMode
    ) {
        this.paperTradingGateway = paperTradingGateway;
        this.zerodhaLiveGateway = zerodhaLiveGateway;
        this.activeMode = new AtomicReference<>(ExecutionMode.valueOf(initialMode.toUpperCase()));
        log.info("BrokerGatewayManager initialized. Active Mode: {}", activeMode.get());
    }

    /**
     * Retrieves the currently active broker gateway.
     */
    public BrokerGateway getActiveGateway() {
        return activeMode.get() == ExecutionMode.LIVE ? zerodhaLiveGateway : paperTradingGateway;
    }

    /**
     * Gets the active execution mode.
     */
    public ExecutionMode getActiveMode() {
        return activeMode.get();
    }

    /**
     * Atomically switches execution mode between PAPER and LIVE.
     * Prevents switching mid-trade if an open position, resting orders,
     * or active strategy state exists.
     * 
     * @param newMode Target execution mode
     * @throws IllegalStateException if a position, resting order, or active state exists
     */
    public synchronized void setExecutionMode(ExecutionMode newMode) {
        if (activeMode.get() == newMode) {
            return;
        }

        // Guard 1: Verify strategy lifecycle is strictly FLAT
        if (strategyProvider != null) {
            var strategy = strategyProvider.getIfAvailable();
            if (strategy != null && strategy.getLifecycleState() != com.algotrade.algotradebackend.strategy.momentum_breakout.TradeLifecycleState.FLAT) {
                throw new IllegalStateException("Cannot switch mode while strategy is actively in state: " + strategy.getLifecycleState());
            }
        }

        // Guard 2: Verify active gateway has no open positions
        if (getActiveGateway().getOpenPosition().isPresent()) {
            throw new IllegalStateException("Cannot switch trading mode while an open position is actively held!");
        }

        // Guard 3: Verify neither gateway has resting active orders
        if (paperTradingGateway.hasActiveOrders() || zerodhaLiveGateway.hasActiveOrders()) {
            throw new IllegalStateException("Cannot switch trading mode while resting broker orders are open!");
        }

        // Guard 4: If switching away from LIVE, verify Zerodha session is alive and authoritative
        if (activeMode.get() == ExecutionMode.LIVE) {
            if (!zerodhaLiveGateway.isSessionReady()) {
                throw new IllegalStateException("Cannot switch from LIVE to PAPER while Zerodha broker session is unverified or disconnected!");
            }
        }

        activeMode.set(newMode);
        log.warn("=== TRADING EXECUTION MODE SWITCHED TO: {} ===", newMode);
    }

    public PaperTradingGateway getPaperTradingGateway() {
        return paperTradingGateway;
    }

    public ZerodhaLiveGateway getZerodhaLiveGateway() {
        return zerodhaLiveGateway;
    }
}
