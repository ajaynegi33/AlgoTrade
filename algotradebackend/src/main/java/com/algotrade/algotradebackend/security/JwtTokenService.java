package com.algotrade.algotradebackend.security;

import org.json.JSONObject;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;

import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.SecureRandom;
import java.time.Instant;
import java.util.*;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Institutional-grade, zero-dependency JWT Token Engine.
 * 
 * <p>Implemented entirely using standard Java Cryptography Architecture (JCA)
 * and Java 25 primitives (javax.crypto.Mac + Base64Url + org.json).
 * 
 * <ul>
 *   <li><b>Access Tokens:</b> 15-minute lifespan, HMAC-SHA256 signed.</li>
 *   <li><b>Refresh Tokens:</b> 24-hour lifespan, cryptographically random, rotated upon every refresh.</li>
 *   <li><b>Refresh Token Rotation (RTR):</b> Detects reuse attacks; if an already-revoked refresh token is presented, all sessions are purged.</li>
 * </ul>
 */
@Service
public class JwtTokenService {

    private static final Logger log = LoggerFactory.getLogger(JwtTokenService.class);
    private static final String HMAC_ALGO = "HmacSHA256";
    private static final long ACCESS_TOKEN_VALIDITY_SECONDS = 15 * 60; // 15 Minutes
    private static final long REFRESH_TOKEN_VALIDITY_SECONDS = 24 * 60 * 60; // 24 Hours (1 Day)

    private final byte[] secretKeyBytes;
    private final SecureRandom secureRandom = new SecureRandom();

    public record RefreshSession(String token, Instant expiresAt, boolean revoked) {}
    public record TokenPair(String accessToken, String refreshToken, long expiresInSeconds) {}

    // In-memory persistent refresh token registry
    private final ConcurrentHashMap<String, RefreshSession> refreshSessions = new ConcurrentHashMap<>();

    public JwtTokenService() {
        this(null, null);
    }

    public JwtTokenService(String configuredSecret) {
        this(configuredSecret, null);
    }

    @org.springframework.beans.factory.annotation.Autowired
    public JwtTokenService(
        @Value("${algotrade.jwt.secret:${algotrade.auth.secret:${ALGOTRADE_JWT_SECRET:}}}") String configuredSecret,
        @org.springframework.beans.factory.annotation.Autowired(required = false) org.springframework.core.env.Environment env
    ) {
        boolean isProd = env != null && env.acceptsProfiles(org.springframework.core.env.Profiles.of("prod", "production"));
        String secret = (configuredSecret != null) ? configuredSecret.trim() : "";

        if (secret.length() >= 32) {
            this.secretKeyBytes = secret.getBytes(StandardCharsets.UTF_8);
            log.info("Initialized JwtTokenService with environment configured HMAC-SHA256 key.");
        } else if (isProd) {
            throw new IllegalStateException(
                "FATAL SECURITY CONFIGURATION ERROR: 'algotrade.jwt.secret' (or ALGOTRADE_JWT_SECRET) must be configured " +
                "with at least 32 characters (256-bit) in production profile. Startup aborted."
            );
        } else {
            byte[] generated = new byte[32];
            new SecureRandom().nextBytes(generated);
            this.secretKeyBytes = generated;
            log.warn("No 256-bit ALGOTRADE_JWT_SECRET provided in environment. Generated ephemeral 256-bit key for this process lifecycle.");
        }
    }

    /**
     * Generates a 15-minute access token for the authenticated operator.
     */
    public String generateAccessToken(String subject, Map<String, Object> extraClaims) {
        long nowSec = Instant.now().getEpochSecond();
        long expSec = nowSec + ACCESS_TOKEN_VALIDITY_SECONDS;

        JSONObject header = new JSONObject();
        header.put("alg", "HS256");
        header.put("typ", "JWT");

        JSONObject payload = new JSONObject();
        payload.put("sub", subject);
        payload.put("role", "ROLE_ADMIN");
        payload.put("iat", nowSec);
        payload.put("exp", expSec);
        payload.put("jti", UUID.randomUUID().toString());

        if (extraClaims != null) {
            for (Map.Entry<String, Object> e : extraClaims.entrySet()) {
                payload.put(e.getKey(), e.getValue());
            }
        }

        String encodedHeader = base64UrlEncode(header.toString().getBytes(StandardCharsets.UTF_8));
        String encodedPayload = base64UrlEncode(payload.toString().getBytes(StandardCharsets.UTF_8));
        String signatureInput = encodedHeader + "." + encodedPayload;

        byte[] signature = signHmacSha256(signatureInput.getBytes(StandardCharsets.UTF_8));
        String encodedSignature = base64UrlEncode(signature);

        return signatureInput + "." + encodedSignature;
    }

    /**
     * Generates a single-use 24-hour cryptographically secure refresh token.
     */
    public String generateRefreshToken() {
        byte[] randomBytes = new byte[32];
        secureRandom.nextBytes(randomBytes);
        String token = HexFormat.of().formatHex(randomBytes);

        Instant expiresAt = Instant.now().plusSeconds(REFRESH_TOKEN_VALIDITY_SECONDS);
        refreshSessions.put(token, new RefreshSession(token, expiresAt, false));
        return token;
    }

    /**
     * Creates an initial access + refresh token pair upon successful authentication.
     */
    public TokenPair issueInitialTokens(String subject) {
        String accessToken = generateAccessToken(subject, null);
        String refreshToken = generateRefreshToken();
        return new TokenPair(accessToken, refreshToken, ACCESS_TOKEN_VALIDITY_SECONDS);
    }

    /**
     * Refresh Token Rotation (RTR).
     * Validates the refresh token, revokes it, and issues a fresh 15-minute access token and new refresh token.
     * Detects reuse attacks: if a revoked refresh token is presented, all sessions are purged.
     */
    public TokenPair rotateRefreshToken(String incomingRefreshToken) {
        if (incomingRefreshToken == null || incomingRefreshToken.isBlank()) {
            return null;
        }

        RefreshSession session = refreshSessions.get(incomingRefreshToken);
        if (session == null) {
            return null; // Unknown token
        }

        if (session.revoked()) {
            log.error("SECURITY ALERT: Reuse of revoked refresh token detected! Possible token theft. Purging all active sessions.");
            refreshSessions.clear();
            return null;
        }

        if (Instant.now().isAfter(session.expiresAt())) {
            refreshSessions.remove(incomingRefreshToken);
            return null; // Expired
        }

        // Revoke the old refresh token
        refreshSessions.put(incomingRefreshToken, new RefreshSession(incomingRefreshToken, session.expiresAt(), true));

        // Issue new pair
        String newAccessToken = generateAccessToken("admin", null);
        String newRefreshToken = generateRefreshToken();

        return new TokenPair(newAccessToken, newRefreshToken, ACCESS_TOKEN_VALIDITY_SECONDS);
    }

    /**
     * Explicitly revokes a refresh token on logout.
     */
    public void revokeRefreshToken(String refreshToken) {
        if (refreshToken != null) {
            RefreshSession s = refreshSessions.get(refreshToken);
            if (s != null) {
                refreshSessions.put(refreshToken, new RefreshSession(s.token(), s.expiresAt(), true));
            }
        }
    }

    /**
     * Validates JWT format, cryptographic signature, and expiration.
     * Returns the parsed claims payload if valid, or null if invalid/expired.
     */
    public Map<String, Object> validateAndExtractClaims(String token) {
        if (token == null || token.isBlank()) return null;

        String[] parts = token.split("\\.");
        if (parts.length != 3) return null;

        String signatureInput = parts[0] + "." + parts[1];
        byte[] expectedSig = signHmacSha256(signatureInput.getBytes(StandardCharsets.UTF_8));
        byte[] actualSig = base64UrlDecode(parts[2]);

        // Timing-attack safe comparison
        if (!MessageDigest.isEqual(expectedSig, actualSig)) {
            return null;
        }

        try {
            String payloadJson = new String(base64UrlDecode(parts[1]), StandardCharsets.UTF_8);
            JSONObject obj = new JSONObject(payloadJson);

            long exp = obj.optLong("exp", 0);
            if (Instant.now().getEpochSecond() >= exp) {
                return null; // Expired
            }

            Map<String, Object> claims = new HashMap<>();
            for (String key : obj.keySet()) {
                claims.put(key, obj.get(key));
            }
            return claims;
        } catch (Exception e) {
            return null;
        }
    }

    public boolean isValidToken(String token) {
        return validateAndExtractClaims(token) != null;
    }

    private byte[] signHmacSha256(byte[] data) {
        try {
            Mac mac = Mac.getInstance(HMAC_ALGO);
            mac.init(new SecretKeySpec(secretKeyBytes, HMAC_ALGO));
            return mac.doFinal(data);
        } catch (Exception e) {
            throw new IllegalStateException("Failed to calculate HMAC-SHA256 signature", e);
        }
    }

    private String base64UrlEncode(byte[] bytes) {
        return Base64.getUrlEncoder().withoutPadding().encodeToString(bytes);
    }

    private byte[] base64UrlDecode(String str) {
        return Base64.getUrlDecoder().decode(str);
    }
}
