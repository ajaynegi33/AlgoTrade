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

    @Test
    void testAuthControllerMeReturns401WhenUnauthenticated() {
        AuthController controller = new AuthController(authService);
        org.springframework.http.ResponseEntity<?> response = controller.me(null);
        assertEquals(org.springframework.http.HttpStatus.UNAUTHORIZED, response.getStatusCode());

        org.springframework.security.core.Authentication anonymousAuth = 
            new org.springframework.security.authentication.AnonymousAuthenticationToken("key", "anonymousUser", 
                java.util.List.of(new org.springframework.security.core.authority.SimpleGrantedAuthority("ROLE_ANONYMOUS")));
        org.springframework.http.ResponseEntity<?> anonResponse = controller.me(anonymousAuth);
        assertEquals(org.springframework.http.HttpStatus.UNAUTHORIZED, anonResponse.getStatusCode());
    }

    @Test
    void testAuthControllerLoginDoesNotContainRefreshTokenInBody() {
        AuthController controller = new AuthController(authService);
        jakarta.servlet.http.HttpServletRequest req = org.mockito.Mockito.mock(jakarta.servlet.http.HttpServletRequest.class);
        jakarta.servlet.http.HttpServletResponse res = org.mockito.Mockito.mock(jakarta.servlet.http.HttpServletResponse.class);

        org.springframework.http.ResponseEntity<?> response = controller.login(
            new AuthController.LoginRequest(TEST_PASSWORD), req, res
        );
        assertEquals(org.springframework.http.HttpStatus.OK, response.getStatusCode());

        @SuppressWarnings("unchecked")
        java.util.Map<String, Object> body = (java.util.Map<String, Object>) response.getBody();
        assertNotNull(body);
        assertTrue(body.containsKey("accessToken"));
        assertFalse(body.containsKey("refreshToken"), "Security violation: refreshToken MUST NOT be leaked in JSON body!");

        // Verify Set-Cookie header was set with HttpOnly
        org.mockito.ArgumentCaptor<String> cookieCaptor = org.mockito.ArgumentCaptor.forClass(String.class);
        org.mockito.Mockito.verify(res).addHeader(org.mockito.Mockito.eq(org.springframework.http.HttpHeaders.SET_COOKIE), cookieCaptor.capture());
        String cookieStr = cookieCaptor.getValue();
        assertTrue(cookieStr.contains("HttpOnly"));
        assertTrue(cookieStr.contains("SameSite=Strict"));
    }
}
