package com.algotrade.algotradebackend.security;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.security.authentication.BadCredentialsException;
import org.springframework.security.crypto.bcrypt.BCryptPasswordEncoder;
import org.springframework.stereotype.Service;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Single-User Password-Based Authentication Service.
 * 
 * <p>Protects against credential leakage and brute-force attacks:
 * <ul>
 *   <li>Master password is read from environment variable ALGOTRADE_AUTH_PASSWORD.</li>
 *   <li>Passwords are BCrypt hashed in memory upon application startup; no plain text matching.</li>
 *   <li>Sliding-window IP rate limiter: maximum 5 failed attempts per 15 minutes per IP.</li>
 * </ul>
 */
@Service
public class AuthService {

    private static final Logger log = LoggerFactory.getLogger(AuthService.class);
    private static final int MAX_FAILED_ATTEMPTS = 5;
    private static final long RATE_LIMIT_WINDOW_MS = 15 * 60 * 1000L; // 15 Minutes

    private final JwtTokenService jwtTokenService;
    private final BCryptPasswordEncoder passwordEncoder = new BCryptPasswordEncoder();
    private final String adminPasswordHash;

    // In-memory sliding window rate limiter: IP -> List of failure timestamps (ms)
    private final ConcurrentHashMap<String, List<Long>> failedAttemptsPerIp = new ConcurrentHashMap<>();

    public AuthService(
        JwtTokenService jwtTokenService,
        @Value("${algotrade.auth.password:${ALGOTRADE_AUTH_PASSWORD:AjayNegi@!#}}") String configuredPassword
    ) {
        this.jwtTokenService = jwtTokenService;
        String rawPassword = (configuredPassword != null && !configuredPassword.isBlank()) 
            ? configuredPassword.trim() 
            : "AjayNegi@!#";
        this.adminPasswordHash = passwordEncoder.encode(rawPassword);
        log.info("Initialized AuthService with BCrypt hashed master operator credentials.");
    }

    /**
     * Authenticates the operator password, checking IP rate limits.
     */
    public JwtTokenService.TokenPair login(String rawPassword, String clientIp) {
        String ip = (clientIp != null && !clientIp.isBlank()) ? clientIp : "UNKNOWN";

        // 1. Check IP rate limit
        long now = System.currentTimeMillis();
        List<Long> attempts = failedAttemptsPerIp.computeIfAbsent(ip, k -> new ArrayList<>());
        synchronized (attempts) {
            attempts.removeIf(t -> (now - t) > RATE_LIMIT_WINDOW_MS);
            if (attempts.size() >= MAX_FAILED_ATTEMPTS) {
                long oldest = attempts.get(0);
                long secondsRemaining = Math.max(1, (RATE_LIMIT_WINDOW_MS - (now - oldest)) / 1000);
                log.warn("BRUTE-FORCE MITIGATION: Locked out IP {} ({} failed attempts). Retry in {}s", ip, attempts.size(), secondsRemaining);
                throw new RateLimitExceededException("Too many failed attempts. Account locked. Please try again in " + secondsRemaining + " seconds.");
            }
        }

        // 2. Verify password with BCrypt
        if (rawPassword == null || !passwordEncoder.matches(rawPassword, adminPasswordHash)) {
            synchronized (attempts) {
                attempts.add(now);
            }
            log.warn("FAILED LOGIN ATTEMPT from IP {}. Remaining attempts: {}", ip, Math.max(0, MAX_FAILED_ATTEMPTS - attempts.size()));
            throw new BadCredentialsException("Invalid operator password.");
        }

        // 3. Clear failed attempts on success
        failedAttemptsPerIp.remove(ip);
        log.info("OPERATOR AUTHENTICATED: Master session established for IP {}", ip);

        // 4. Issue initial 15-minute access token and 24-hour refresh token
        return jwtTokenService.issueInitialTokens("admin");
    }

    /**
     * Rotates refresh token and issues fresh 15-minute access token.
     */
    public JwtTokenService.TokenPair refresh(String incomingRefreshToken) {
        JwtTokenService.TokenPair pair = jwtTokenService.rotateRefreshToken(incomingRefreshToken);
        if (pair == null) {
            throw new BadCredentialsException("Invalid, expired, or reused refresh token.");
        }
        return pair;
    }

    /**
     * Revokes active refresh session on logout.
     */
    public void logout(String refreshToken) {
        jwtTokenService.revokeRefreshToken(refreshToken);
    }

    public static class RateLimitExceededException extends RuntimeException {
        public RateLimitExceededException(String message) {
            super(message);
        }
    }
}
