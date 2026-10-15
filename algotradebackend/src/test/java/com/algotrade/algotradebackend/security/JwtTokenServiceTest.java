package com.algotrade.algotradebackend.security;

import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import java.time.Instant;
import java.util.Map;
import java.util.Optional;

import static org.junit.jupiter.api.Assertions.*;

class JwtTokenServiceTest {

    private JwtTokenService tokenService;

    @BeforeEach
    void setUp() {
        tokenService = new JwtTokenService();
    }

    @Test
    void testGenerateAndValidateAccessToken() {
        String token = tokenService.generateAccessToken("trader", Map.of("role", "ROLE_ADMIN"));
        assertNotNull(token);
        assertTrue(tokenService.isValidToken(token));

        Map<String, Object> claims = tokenService.validateAndExtractClaims(token);
        assertNotNull(claims);
        assertEquals("trader", claims.get("sub"));
        assertEquals("ROLE_ADMIN", claims.get("role"));
        assertTrue(claims.containsKey("exp"));
        assertTrue(claims.containsKey("iat"));
    }

    @Test
    void testTamperedTokenIsRejected() {
        String token = tokenService.generateAccessToken("trader", null);
        String[] parts = token.split("\\.");
        assertEquals(3, parts.length);

        // Tamper with payload
        String tampered = parts[0] + "." + parts[1] + "tampered." + parts[2];
        assertFalse(tokenService.isValidToken(tampered));
        assertNull(tokenService.validateAndExtractClaims(tampered));
    }

    @Test
    void testInvalidStructureTokenIsRejected() {
        assertFalse(tokenService.isValidToken("invalid-token-string"));
        assertFalse(tokenService.isValidToken(null));
        assertFalse(tokenService.isValidToken(""));
        assertNull(tokenService.validateAndExtractClaims("part1.part2"));
    }

    @Test
    void testRefreshTokenLifecycleAndRotation() {
        String refreshToken = tokenService.generateRefreshToken();
        assertNotNull(refreshToken);
        assertEquals(64, refreshToken.length()); // 32 bytes hex encoded

        // Rotate token
        JwtTokenService.TokenPair newPair = tokenService.rotateRefreshToken(refreshToken);
        assertNotNull(newPair);
        assertNotNull(newPair.accessToken());
        assertNotNull(newPair.refreshToken());
        assertNotEquals(refreshToken, newPair.refreshToken());
        assertTrue(tokenService.isValidToken(newPair.accessToken()));

        // Old refresh token must be invalidated (one-time use RTR guarantee)
        JwtTokenService.TokenPair replayPair = tokenService.rotateRefreshToken(refreshToken);
        assertNull(replayPair, "Replaying an already used refresh token must be rejected");

        // Revoke new refresh token
        tokenService.revokeRefreshToken(newPair.refreshToken());
        JwtTokenService.TokenPair afterRevokePair = tokenService.rotateRefreshToken(newPair.refreshToken());
        assertNull(afterRevokePair, "Revoked refresh token must be rejected");
    }
}
