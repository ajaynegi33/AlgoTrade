package com.algotrade.algotradebackend.core.model;

/**
 * Execution Mode for the trading engine.
 * 
 * <p>PAPER: Zero financial risk. Simulates fills against live or mock ticks,
 * logs trades to `algotradepaper.db`, and calculates exact hypothetical taxes and slippage.</p>
 * 
 * <p>LIVE: Real capital at risk. Dispatches orders directly to Zerodha Kite Connect 4.x.</p>
 */
public enum ExecutionMode {
    PAPER,
    LIVE
}
