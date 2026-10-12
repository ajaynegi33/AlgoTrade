package com.algotrade.algotradebackend.strategy.momentum_breakout;

/**
 * Explicit state machine for tracking order and position lifecycle in MomentumBreakoutStrategy.
 * Prevents race conditions where in-flight orders are abandoned by transient tick events.
 */
public enum TradeLifecycleState {
    /**
     * No active position or order in flight. Strategy ready to evaluate or standing down.
     */
    FLAT,

    /**
     * Entry limit order submitted to broker, waiting for exchange fill confirmation.
     */
    PENDING_ENTRY,

    /**
     * Entry confirmed filled at broker, protective stop-loss placed and active.
     */
    ACTIVE_POSITION,

    /**
     * Mandatory 15:05 square-off or exit submitted, waiting for flat confirmation from broker.
     */
    PENDING_EXIT
}
