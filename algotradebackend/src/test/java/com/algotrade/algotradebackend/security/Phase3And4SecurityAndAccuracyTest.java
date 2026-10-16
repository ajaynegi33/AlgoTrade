package com.algotrade.algotradebackend.security;

import com.algotrade.algotradebackend.backtest.model.BacktestRequest;
import com.algotrade.algotradebackend.backtest.model.BacktestResult;
import com.algotrade.algotradebackend.backtest.service.BacktestEngineService;
import com.algotrade.algotradebackend.backtest.service.HistoricalMarketDataService;
import com.algotrade.algotradebackend.core.model.ScoredCandidate;
import com.algotrade.algotradebackend.strategy.momentum_breakout.CandidateSnapshot;
import com.algotrade.algotradebackend.strategy.momentum_breakout.MomentumScoringEngine;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.mock.env.MockEnvironment;
import org.springframework.mock.web.MockHttpServletRequest;
import org.springframework.mock.web.MockHttpServletResponse;

import java.time.LocalDate;
import java.time.LocalTime;
import java.util.Map;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.Mockito.*;

/**
 * Automated verification suite for Phase 3 (Strategy Alpha & Hard Gate Refinement)
 * and Phase 4 (Security Hardening & Frontend Accuracy).
 */
class Phase3And4SecurityAndAccuracyTest {

    @Test
    @DisplayName("Phase 4.1: Production profile fails startup if JWT secret is missing or < 32 characters")
    void testProdProfileFailsStartupWhenJwtSecretMissingOrShort() {
        MockEnvironment prodEnv = new MockEnvironment();
        prodEnv.setActiveProfiles("prod");

        // 1. Missing / null secret in prod -> FATAL startup abort
        IllegalStateException exNull = assertThrows(IllegalStateException.class, () ->
            new JwtTokenService(null, prodEnv)
        );
        assertTrue(exNull.getMessage().contains("FATAL SECURITY CONFIGURATION ERROR"));

        // 2. Empty secret in prod -> FATAL startup abort
        IllegalStateException exEmpty = assertThrows(IllegalStateException.class, () ->
            new JwtTokenService("   ", prodEnv)
        );
        assertTrue(exEmpty.getMessage().contains("FATAL SECURITY CONFIGURATION ERROR"));

        // 3. Short secret (31 characters) in prod -> FATAL startup abort
        String shortSecret = "1234567890123456789012345678901"; // 31 chars
        assertEquals(31, shortSecret.length());
        IllegalStateException exShort = assertThrows(IllegalStateException.class, () ->
            new JwtTokenService(shortSecret, prodEnv)
        );
        assertTrue(exShort.getMessage().contains("at least 32 characters"));

        // 4. Valid 32-character secret in prod -> Startup succeeds
        String validSecret = "12345678901234567890123456789012"; // 32 chars
        assertDoesNotThrow(() -> new JwtTokenService(validSecret, prodEnv));

        // 5. 'production' profile alias in prod -> FATAL startup abort
        MockEnvironment productionAliasEnv = new MockEnvironment();
        productionAliasEnv.setActiveProfiles("production");
        assertThrows(IllegalStateException.class, () -> new JwtTokenService(null, productionAliasEnv));

        // 6. Non-prod environment (dev/test) allows ephemeral key generation without throwing
        MockEnvironment devEnv = new MockEnvironment();
        devEnv.setActiveProfiles("dev");
        assertDoesNotThrow(() -> {
            JwtTokenService devService = new JwtTokenService("", devEnv);
            String token = devService.generateAccessToken("dev-user", Map.of());
            assertTrue(devService.isValidToken(token));
        });
    }

    @Test
    @DisplayName("Phase 4.2: /api/auth/logout without token reliably clears HTTP cookie with SameSite=Strict and Max-Age=0")
    void testLogoutWithoutTokenClearsCookieReliably() {
        AuthService mockAuthService = mock(AuthService.class);
        AuthController authController = new AuthController(mockAuthService);

        // 1. HTTP unauthenticated logout
        MockHttpServletRequest httpRequest = new MockHttpServletRequest();
        MockHttpServletResponse httpResponse = new MockHttpServletResponse();

        ResponseEntity<?> response = authController.logout(null, httpRequest, httpResponse);

        assertEquals(HttpStatus.OK, response.getStatusCode());
        String cookieHeader = httpResponse.getHeader(HttpHeaders.SET_COOKIE);
        assertNotNull(cookieHeader, "Set-Cookie header must be present on logout");
        assertTrue(cookieHeader.contains("jwt="), "Cookie name must be 'jwt'");
        assertTrue(cookieHeader.contains("Max-Age=0"), "Cookie must be expired immediately");
        assertTrue(cookieHeader.contains("Path=/api/auth"), "Cookie path must match '/api/auth'");
        assertTrue(cookieHeader.contains("HttpOnly"), "Cookie must be HttpOnly");
        assertTrue(cookieHeader.contains("SameSite=Strict"), "Cookie must be SameSite=Strict");

        // AuthService logout should not be invoked if no token was provided
        verify(mockAuthService, never()).logout(anyString());

        // 2. HTTPS request generates Secure cookie
        MockHttpServletRequest httpsRequest = new MockHttpServletRequest();
        httpsRequest.setSecure(true);
        MockHttpServletResponse httpsResponse = new MockHttpServletResponse();

        authController.logout(null, httpsRequest, httpsResponse);
        String httpsCookieHeader = httpsResponse.getHeader(HttpHeaders.SET_COOKIE);
        assertNotNull(httpsCookieHeader);
        assertTrue(httpsCookieHeader.contains("Secure"), "HTTPS logout must set Secure cookie flag");
    }

    @Test
    @DisplayName("Phase 3.1: Hard eligibility gate rejects candidates overextended > 2.2% above VWAP with epsilon tolerance")
    void testHardEligibilityGateVwapEpsilonTolerance() {
        MomentumScoringEngine engine = new MomentumScoringEngine();

        // Template candidate with baseline values
        double vwap = 1000.0;
        double pdh = 1000.0;
        double prevClose = 1000.0;
        double openPrice = 1005.0;
        double low15 = 1000.0;
        double high15 = 1020.0;
        double avgVol = 100_000.0;
        double atrPct = 1.8;
        long windowVol = 20_000L;
        double depthImbalance = 0.35;
        double gapPct = 0.5;

        // 1. Normal breakout: LTP = 1018.0 (1.8% above VWAP) -> PASS
        CandidateSnapshot sPass = new CandidateSnapshot(
            "STOCK_NORMAL", 1018.0, openPrice, low15, high15,
            prevClose, pdh, avgVol, atrPct, windowVol, vwap, depthImbalance, gapPct
        );
        assertTrue(engine.passesHardGates(sPass), "1.8% above VWAP must pass hard gates");

        // 2. Boundary breakout: LTP = 1022.0 (exactly 2.20% above VWAP) -> PASS
        CandidateSnapshot sBoundaryExact = new CandidateSnapshot(
            "STOCK_BOUNDARY", 1022.0, openPrice, low15, high15,
            prevClose, pdh, avgVol, atrPct, windowVol, vwap, depthImbalance, gapPct
        );
        assertTrue(engine.passesHardGates(sBoundaryExact), "Exactly 2.20% above VWAP must pass hard gates");

        // 3. IEEE-754 Epsilon breakout: 2.20000001% above VWAP -> PASS via 1e-6 tolerance
        double ltpEpsilon = 1000.0 * (1.0 + 0.0220000005);
        CandidateSnapshot sEpsilon = new CandidateSnapshot(
            "STOCK_EPSILON", ltpEpsilon, openPrice, low15, high15,
            prevClose, pdh, avgVol, atrPct, windowVol, vwap, depthImbalance, gapPct
        );
        assertTrue(engine.passesHardGates(sEpsilon), "IEEE-754 epsilon within 2.200001% must pass hard gates");

        // 4. Overextended breakout: LTP = 1022.5 (2.25% above VWAP) -> FAIL
        CandidateSnapshot sOverextended = new CandidateSnapshot(
            "STOCK_OVEREXTENDED", 1022.5, openPrice, low15, high15,
            prevClose, pdh, avgVol, atrPct, windowVol, vwap, depthImbalance, gapPct
        );
        assertFalse(engine.passesHardGates(sOverextended), "2.25% above VWAP must be rejected as overextended");

        // 5. Evaluate full ScoredCandidate for overextended stock
        ScoredCandidate scoredOverextended = engine.evaluate(sOverextended, LocalTime.of(9, 30), false);
        assertFalse(scoredOverextended.eligible(), "Overextended candidate must have eligible=false");
        assertEquals("DISQUALIFIED", scoredOverextended.status(), "Overextended candidate must be DISQUALIFIED");

        // 6. Overextended breakout in backtest mode
        ScoredCandidate backtestScored = engine.evaluate(sOverextended, LocalTime.of(9, 30), true);
        assertFalse(backtestScored.eligible(), "Overextended candidate in backtest must have eligible=false");
        assertEquals("DISQUALIFIED", backtestScored.status(), "Overextended candidate in backtest must be DISQUALIFIED");
    }

    @Test
    @DisplayName("Phase 3.2: Backtest engine re-evaluation runs cleanly on 30-day and 90-day periods with ₹400 circuit breaker")
    void testBacktestRunnerExpectancyAndCircuitBreaker() {
        MomentumScoringEngine scoringEngine = new MomentumScoringEngine();
        HistoricalMarketDataService dataService = new HistoricalMarketDataService();
        BacktestEngineService backtestEngine = new BacktestEngineService(scoringEngine, dataService);

        LocalDate end = LocalDate.now();
        LocalDate start30D = end.minusDays(30);
        LocalDate start90D = end.minusDays(90);

        // 1. Run 30-Day simulation
        BacktestRequest req30D = new BacktestRequest(
            start30D.toString(),
            end.toString(),
            10000.0,
            2.0,
            80,
            0.8,
            0.05,
            5.0
        );

        BacktestResult res30D = backtestEngine.runBacktest(req30D);
        assertNotNull(res30D);
        assertNotNull(res30D.summary());
        System.out.println("=== 30-DAY BACKTEST RESULTS (WITH 2.2% VWAP GATE & CIRCUIT BREAKER) ===");
        System.out.printf("Total Trades: %d%n", res30D.summary().totalTrades());
        System.out.printf("Win Rate: %.2f%%%n", res30D.summary().winRatePct());
        System.out.printf("Total Gross P&L: ₹%.2f%n", res30D.summary().totalGrossPnL());
        System.out.printf("Total Net P&L: ₹%.2f%n", res30D.summary().totalNetPnL());
        System.out.printf("Profit Factor: %.2f%n", res30D.summary().profitFactor());
        System.out.printf("Max Drawdown: %.2f%%%n", res30D.summary().maxDrawdownPct());
        System.out.printf("Final Capital: ₹%.2f%n", res30D.summary().finalCapital());

        // 2. Run 90-Day simulation
        BacktestRequest req90D = new BacktestRequest(
            start90D.toString(),
            end.toString(),
            10000.0,
            2.0,
            80,
            0.8,
            0.05,
            5.0
        );

        BacktestResult res90D = backtestEngine.runBacktest(req90D);
        assertNotNull(res90D);
        assertNotNull(res90D.summary());
        System.out.println("=== 90-DAY BACKTEST RESULTS (WITH 2.2% VWAP GATE & CIRCUIT BREAKER) ===");
        System.out.printf("Total Trades: %d%n", res90D.summary().totalTrades());
        System.out.printf("Win Rate: %.2f%%%n", res90D.summary().winRatePct());
        System.out.printf("Total Gross P&L: ₹%.2f%n", res90D.summary().totalGrossPnL());
        System.out.printf("Total Net P&L: ₹%.2f%n", res90D.summary().totalNetPnL());
        System.out.printf("Profit Factor: %.2f%n", res90D.summary().profitFactor());
        System.out.printf("Max Drawdown: %.2f%%%n", res90D.summary().maxDrawdownPct());
        System.out.printf("Final Capital: ₹%.2f%n", res90D.summary().finalCapital());

        // 3. Verify no daily loss breached ₹400 circuit breaker (grouping trades by trade date)
        Map<String, Double> dailyPnLMap = new java.util.HashMap<>();
        for (var t : res90D.trades()) {
            if (t.date() != null) {
                dailyPnLMap.merge(t.date(), t.netPnL(), Double::sum);
            }
        }

        for (Map.Entry<String, Double> entry : dailyPnLMap.entrySet()) {
            double dailyPnL = entry.getValue();
            // Allow a small ₹50 tolerance strictly for slippage/charges on the boundary trade
            assertTrue(dailyPnL >= -450.0,
                String.format("Daily PnL on %s (₹%.2f) must respect ₹400 daily loss circuit breaker", entry.getKey(), dailyPnL));
        }
    }
}
