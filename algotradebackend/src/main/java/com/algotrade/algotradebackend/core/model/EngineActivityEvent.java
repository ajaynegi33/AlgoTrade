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
    String category, // "STRATEGY", "RISK", "ORDER", "LIFECYCLE", "SYSTEM"
    String level,    // "INFO", "SUCCESS", "WARN", "ERROR"
    String title,
    String message,
    String symbol,
    Map<String, Object> metadata
) {}
