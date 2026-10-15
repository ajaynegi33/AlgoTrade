package com.algotrade.algotradebackend.security;

import jakarta.servlet.http.Cookie;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseCookie;
import org.springframework.http.ResponseEntity;
import org.springframework.security.authentication.BadCredentialsException;
import org.springframework.web.bind.annotation.*;

import java.util.Map;

/**
 * REST API Controller managing operator login, token refresh, and session termination.
 */
@RestController
@RequestMapping("/api/auth")
public class AuthController {

    private final AuthService authService;
    private static final String REFRESH_COOKIE_NAME = "algotrade_refresh_token";

    public record LoginRequest(String password) {}
    public record RefreshRequest(String refreshToken) {}

    public AuthController(AuthService authService) {
        this.authService = authService;
    }

    @PostMapping("/login")
    public ResponseEntity<?> login(
        @RequestBody(required = false) LoginRequest request,
        HttpServletRequest httpRequest,
        HttpServletResponse httpResponse
    ) {
        if (request == null || request.password() == null || request.password().isBlank()) {
            return ResponseEntity.badRequest().body(Map.of("error", "Password is required"));
        }

        String clientIp = extractClientIp(httpRequest);
        try {
            JwtTokenService.TokenPair tokenPair = authService.login(request.password(), clientIp);

            boolean isHttps = httpRequest.isSecure() || "https".equalsIgnoreCase(httpRequest.getHeader("X-Forwarded-Proto"));
            // Set refresh token in HttpOnly Cookie (SameSite=Strict, 24h Max-Age)
            ResponseCookie refreshCookie = ResponseCookie.from(REFRESH_COOKIE_NAME, tokenPair.refreshToken())
                .httpOnly(true)
                .secure(isHttps)
                .path("/api/auth")
                .maxAge(24 * 60 * 60)
                .sameSite("Strict")
                .build();
            httpResponse.addHeader(HttpHeaders.SET_COOKIE, refreshCookie.toString());

            return ResponseEntity.ok(Map.of(
                "accessToken", tokenPair.accessToken(),
                "expiresIn", tokenPair.expiresInSeconds(),
                "tokenType", "Bearer"
            ));
        } catch (AuthService.RateLimitExceededException rle) {
            return ResponseEntity.status(HttpStatus.TOO_MANY_REQUESTS).body(Map.of("error", rle.getMessage()));
        } catch (BadCredentialsException bce) {
            return ResponseEntity.status(HttpStatus.UNAUTHORIZED).body(Map.of("error", bce.getMessage()));
        }
    }

    @PostMapping("/refresh")
    public ResponseEntity<?> refresh(
        @RequestBody(required = false) RefreshRequest request,
        HttpServletRequest httpRequest,
        HttpServletResponse httpResponse
    ) {
        String token = null;
        if (request != null && request.refreshToken() != null && !request.refreshToken().isBlank()) {
            token = request.refreshToken().trim();
        } else if (httpRequest.getCookies() != null) {
            for (Cookie c : httpRequest.getCookies()) {
                if (REFRESH_COOKIE_NAME.equals(c.getName())) {
                    token = c.getValue();
                    break;
                }
            }
        }

        if (token == null || token.isBlank()) {
            return ResponseEntity.status(HttpStatus.UNAUTHORIZED).body(Map.of("error", "No refresh token provided"));
        }

        try {
            JwtTokenService.TokenPair tokenPair = authService.refresh(token);
            boolean isHttps = httpRequest.isSecure() || "https".equalsIgnoreCase(httpRequest.getHeader("X-Forwarded-Proto"));

            ResponseCookie refreshCookie = ResponseCookie.from(REFRESH_COOKIE_NAME, tokenPair.refreshToken())
                .httpOnly(true)
                .secure(isHttps)
                .path("/api/auth")
                .maxAge(24 * 60 * 60)
                .sameSite("Strict")
                .build();
            httpResponse.addHeader(HttpHeaders.SET_COOKIE, refreshCookie.toString());

            return ResponseEntity.ok(Map.of(
                "accessToken", tokenPair.accessToken(),
                "expiresIn", tokenPair.expiresInSeconds(),
                "tokenType", "Bearer"
            ));
        } catch (BadCredentialsException bce) {
            return ResponseEntity.status(HttpStatus.UNAUTHORIZED).body(Map.of("error", bce.getMessage()));
        }
    }

    @PostMapping("/logout")
    public ResponseEntity<?> logout(
        @RequestBody(required = false) RefreshRequest request,
        HttpServletRequest httpRequest,
        HttpServletResponse httpResponse
    ) {
        String token = null;
        if (request != null && request.refreshToken() != null) {
            token = request.refreshToken();
        } else if (httpRequest.getCookies() != null) {
            for (Cookie c : httpRequest.getCookies()) {
                if (REFRESH_COOKIE_NAME.equals(c.getName())) {
                    token = c.getValue();
                    break;
                }
            }
        }

        if (token != null) {
            authService.logout(token);
        }

        ResponseCookie clearCookie = ResponseCookie.from(REFRESH_COOKIE_NAME, "")
            .httpOnly(true)
            .path("/api/auth")
            .maxAge(0)
            .build();
        httpResponse.addHeader(HttpHeaders.SET_COOKIE, clearCookie.toString());

        return ResponseEntity.ok(Map.of("success", true, "message", "Logged out successfully"));
    }

    @GetMapping("/me")
    public ResponseEntity<?> me(org.springframework.security.core.Authentication authentication) {
        if (authentication == null || !authentication.isAuthenticated() 
            || authentication instanceof org.springframework.security.authentication.AnonymousAuthenticationToken) {
            return ResponseEntity.status(HttpStatus.UNAUTHORIZED).body(Map.of(
                "authenticated", false,
                "error", "Unauthorized"
            ));
        }
        return ResponseEntity.ok(Map.of(
            "authenticated", true,
            "username", authentication.getName(),
            "role", "admin"
        ));
    }

    private String extractClientIp(HttpServletRequest request) {
        String xf = request.getHeader("X-Forwarded-For");
        if (xf != null && !xf.isBlank()) {
            return xf.split(",")[0].trim();
        }
        return request.getRemoteAddr();
    }
}
