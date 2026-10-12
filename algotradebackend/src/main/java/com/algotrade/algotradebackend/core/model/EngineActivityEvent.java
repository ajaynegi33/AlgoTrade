package com.algotrade.algotradebackend.core.model;

import java.time.Instant;
import java.util.Map;

/**
 * Immutable telemetry event emitted by the algorithmic engine whenever an evaluation,
 * risk check, order placement, or lifecycle decision is made.
 */
public record EngineActivityEvent(
    long id,
    Instant timestamp,
    String category,      // "STRATEGY", "RISK", "ORDERS", "LIFECYCLE", "SYSTEM"
    String level,         // "INFO", "SUCCESS", "WARN", "ERROR"
    String executionMode, // "PAPER" or "LIVE"
    String tradeId,       // Optional unique identifier correlating events belonging to the same trade lifecycle
    String title,
    String message,
    String symbol,
    Map<String, Object> metadata
) {}
