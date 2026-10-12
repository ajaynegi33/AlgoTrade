package com.algotrade.algotradebackend.core.model;

/**
 * Data container representing a scanned stock evaluated by the scoring engine.
 * Displayed directly on the Next.js frontend "Top 10 Performers & Scoring Leaderboard" tab.
 */
public record ScoredCandidate(
    int rank,
    String symbol,
    double ltp,
    double gapPct,
    int rvolScore,           // Max 30
    int vwapScore,           // Max 25
    int depthImbalanceScore, // Max 25
    int pdhScore,            // Max 20
    int totalScore,          // Max 100
    String status            // "SELECTED FOR TRADE", "DISQUALIFIED", "RUNNER UP"
) {
    public boolean isQualified() {
        return totalScore >= 75;
    }
}
