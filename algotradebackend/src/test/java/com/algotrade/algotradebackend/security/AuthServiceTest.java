package com.algotrade.algotradebackend.security;

import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.security.authentication.BadCredentialsException;

import static org.junit.jupiter.api.Assertions.*;

class AuthServiceTest {

    private JwtTokenService jwtTokenService;
    private AuthService authService;
    private static final String TEST_PASSWORD = "TestMasterPassword@123";

    @BeforeEach
    void setUp() {
        jwtTokenService = new JwtTokenService();
        authService = new AuthService(jwtTokenService, TEST_PASSWORD);
    }

    @Test
    void testSuccessfulLogin() {
        JwtTokenService.TokenPair tokens = authService.login(TEST_PASSWORD, "127.0.0.1");
        assertNotNull(tokens);
        assertNotNull(tokens.accessToken());
        assertNotNull(tokens.refreshToken());
        assertEquals(900, tokens.expiresInSeconds());
        assertTrue(jwtTokenService.isValidToken(tokens.accessToken()));
    }

    @Test
    void testFailedLoginThrowsBadCredentials() {
        assertThrows(BadCredentialsException.class, () -> 
            authService.login("WrongPassword!", "127.0.0.1")
        );
    }

    @Test
    void testRateLimitLocksOutAfterFiveFailedAttempts() {
        String testIp = "192.168.1.100";
        for (int i = 0; i < 5; i++) {
            assertThrows(BadCredentialsException.class, () -> 
                authService.login("WrongPassword", testIp)
            );
        }

        // 6th attempt must be locked out with RateLimitExceededException even if password is now correct
        assertThrows(AuthService.RateLimitExceededException.class, () -> 
            authService.login(TEST_PASSWORD, testIp)
        );
    }

    @Test
    void testRefreshTokenRotationFlow() {
        JwtTokenService.TokenPair initial = authService.login(TEST_PASSWORD, "127.0.0.1");
        assertNotNull(initial);

        JwtTokenService.TokenPair refreshed = authService.refresh(initial.refreshToken());
        assertNotNull(refreshed);
        assertNotNull(refreshed.accessToken());
        assertNotNull(refreshed.refreshToken());
        assertNotEquals(initial.refreshToken(), refreshed.refreshToken());

        // Attempting to reuse old refresh token must fail
        assertThrows(BadCredentialsException.class, () -> 
            authService.refresh(initial.refreshToken())
        );
    }

    @Test
    void testLogoutRevokesSession() {
        JwtTokenService.TokenPair tokens = authService.login(TEST_PASSWORD, "127.0.0.1");
        authService.logout(tokens.refreshToken());

        // Refreshing with revoked token must fail
        assertThrows(BadCredentialsException.class, () -> 
            authService.refresh(tokens.refreshToken())
        );
    }
}
