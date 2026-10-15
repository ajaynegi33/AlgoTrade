package com.algotrade.algotradebackend.strategy.momentum_breakout;

import com.algotrade.algotradebackend.core.model.ScoredCandidate;
import org.springframework.stereotype.Component;

import java.util.Comparator;
import java.util.List;

/**
 * Pure mathematical scoring engine for the Momentum Breakout Strategy.
 * 
 * <p>Implements the complete 100-point rubric with zero undefined bands:
 * <ul>
 *   <li><b>RVOL (Max 30 pts):</b> Relative opening volume expansion.</li>
 *   <li><b>VWAP Distance (Max 25 pts):</b> Price above fair value without overextension.</li>
 *   <li><b>Depth Imbalance (Max 25 pts):</b> Order-book buyers outnumbering sellers.</li>
 *   <li><b>PDH Breakout & ATR (Max 20 pts):</b> Price above yesterday's high with sufficient daily volatility.</li>
 * </ul>
 * </p>
 */
@Component
public class MomentumScoringEngine {

    /**
     * Evaluates a single candidate from CandidateMetrics by extracting an atomic snapshot.
     */
    public ScoredCandidate evaluate(CandidateMetrics m) {
        return evaluate(m.getSnapshot(), java.time.LocalTime.of(9, 30));
    }

    public ScoredCandidate evaluate(CandidateMetrics m, java.time.LocalTime currentTime) {
        return evaluate(m.getSnapshot(), currentTime);
    }

    /**
     * Evaluates a single candidate snapshot and outputs an immutable ScoredCandidate record.
     * Enforces mandatory hard eligibility gates (PDH breakout, above VWAP, positive volume)
     * independently of points awarded.
     */
    public ScoredCandidate evaluate(CandidateSnapshot s) {
        return evaluate(s, java.time.LocalTime.of(9, 30));
    }

    public ScoredCandidate evaluate(CandidateSnapshot s, java.time.LocalTime currentTime) {
        // Metric 1: Time-Normalized RVOL Score (Max 30)
        double expectedVol = calculateExpectedCumulativeVolume(s.avgDailyVolume10Day(), currentTime);
        double rvol = expectedVol > 0 ? (double) s.windowVolume() / expectedVol : 0.0;
        int rvolScore = calculateRvolScore(rvol);

        // Metric 2: VWAP Distance Score (Max 25)
        double vwap = s.vwap();
        double ltp = s.ltp();
        int vwapScore = calculateVwapScore(ltp, vwap);

        // Metric 3: Depth Imbalance Score (Max 25)
        double di = s.depthImbalance();
        int depthScore = calculateDepthScore(di);

        // Metric 4: PDH & HOD Breakout & ATR Score (Max 20)
        int pdhScore = calculatePdhAndHodScore(ltp, s.previousDayHigh(), s.high15(), s.atrPct());

        int totalScore = rvolScore + vwapScore + depthScore + pdhScore;

        // Hard Eligibility Gate: Price must be strictly above VWAP and strictly breaking previous day high
        boolean eligible = passesHardGates(s);
        String status = (eligible && totalScore >= 75) ? "QUALIFIED" : "DISQUALIFIED";

        return new ScoredCandidate(
            0, // Rank to be populated after sorting
            s.symbol(),
            ltp,
            Math.round(s.gapPct() * 100.0) / 100.0,
            rvolScore,
            vwapScore,
            depthScore,
            pdhScore,
            totalScore,
            eligible,
            status
        );
    }

    /**
     * Strict hard eligibility gates.
     * Candidates must satisfy all mandatory conditions to be eligible for trading.
     */
    public boolean passesHardGates(CandidateSnapshot s) {
        return s.ltp() > 0
            && s.vwap() > 0
            && s.previousDayHigh() > 0
            && s.ltp() > s.vwap()                // Must be trading above VWAP
            && s.ltp() > s.previousDayHigh()     // Breakout above PDH
            && (s.prevClose() <= 0 || s.ltp() >= s.prevClose()) // Cannot be down on the day
            && s.gapPct() >= -0.10               // Cannot have significant negative pre-open gap
            && s.windowVolume() > 0
            && s.avgDailyVolume10Day() > 0;
    }

    /**
     * Ranks a list of scored candidates using the 4-level deterministic tie-breaker,
     * prioritizing eligible candidates first so that an ineligible stock with a higher score
     * cannot block a valid candidate.
     */
    public List<ScoredCandidate> rankCandidates(List<ScoredCandidate> rawList) {
        Comparator<ScoredCandidate> comparator = Comparator
            .comparing(ScoredCandidate::eligible).reversed() // Eligible candidates ranked above ineligible
            .thenComparing(Comparator.comparingInt(ScoredCandidate::totalScore).reversed())
            .thenComparing(Comparator.comparingInt(ScoredCandidate::rvolScore).reversed())
            .thenComparing(Comparator.comparingInt(ScoredCandidate::depthImbalanceScore).reversed())
            .thenComparing(ScoredCandidate::symbol); // Alphabetical deterministic tie-breaker

        List<ScoredCandidate> sorted = rawList.stream()
            .sorted(comparator)
            .toList();

        // Assign ranks (1 to N) and update top pick status
        return java.util.stream.IntStream.range(0, sorted.size())
            .mapToObj(i -> {
                ScoredCandidate c = sorted.get(i);
                int rank = i + 1;
                String status = c.status();
                if (c.isQualified()) {
                    status = rank == 1 ? "SELECTED FOR TRADE" : "RUNNER UP";
                }
                return new ScoredCandidate(
                    rank, c.symbol(), c.ltp(), c.gapPct(),
                    c.rvolScore(), c.vwapScore(), c.depthImbalanceScore(), c.pdhScore(),
                    c.totalScore(), c.eligible(), status
                );
            })
            .toList();
    }

    private int calculateRvolScore(double rvol) {
        if (rvol >= 3.00) return 30;
        if (rvol >= 2.00) return 20;
        if (rvol >= 1.50) return 10;
        return 0;
    }

    private int calculateVwapScore(double ltp, double vwap) {
        // Hard Gate: Price must be above VWAP
        if (ltp <= vwap || vwap <= 0) return 0;

        double vdPct = ((ltp - vwap) / vwap) * 100.0;
        if (vdPct >= 0.30 && vdPct <= 1.80) return 25; // Ideal expansion
        if (vdPct > 0.00 && vdPct < 0.30) return 10;   // Weak expansion
        if (vdPct > 1.80 && vdPct <= 2.20) return 10;  // Stretched
        return 0; // Overextended (> 2.2%) mean-reversion trap
    }

    private int calculateDepthScore(double di) {
        if (di >= 0.35) return 25;  // Buyers >= 2x Sellers
        if (di >= 0.15) return 15;
        if (di >= 0.00) return 5;
        return 0; // Sellers dominate
    }

    public double calculateExpectedCumulativeVolume(double avgDailyVolume10Day, java.time.LocalTime time) {
        if (avgDailyVolume10Day <= 0) return 0.0;
        if (time == null || time.isBefore(java.time.LocalTime.of(9, 30))) {
            return avgDailyVolume10Day * 0.10; // Opening 15-min baseline
        }
        // NSE Equities session: 09:15 to 15:30 = 375 trading minutes
        int minutesElapsed = (time.getHour() * 60 + time.getMinute()) - (9 * 60 + 15);
        double fraction = Math.min(1.0, Math.max(0.10, minutesElapsed / 375.0));
        return avgDailyVolume10Day * fraction;
    }

    private int calculatePdhAndHodScore(double ltp, double pdh, double hod, double atrPct) {
        if (ltp > pdh && pdh > 0) {
            boolean isHodBreakout = hod > 0 && ltp >= hod * 0.998;
            if (isHodBreakout && atrPct >= 1.80) return 20;
            if (isHodBreakout || atrPct >= 1.80) return 15;
            return 10;
        }
        return 0;
    }

    // Retain legacy method for testing backward compatibility
    int calculatePdhScore(double ltp, double pdh, double atrPct) {
        if (ltp > pdh && pdh > 0) {
            return atrPct >= 1.80 ? 20 : 10;
        }
        return 0;
    }
}
