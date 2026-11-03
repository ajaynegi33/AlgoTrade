package auth

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/ajaynegi33/AlgoTrade/internal/response"
)

// Zero-allocation context key (empty struct)
type userCtxKey struct{}

func GetUserFromContext(ctx context.Context) (*CustomClaims, bool) {
	claims, ok := ctx.Value(userCtxKey{}).(*CustomClaims)
	return claims, ok
}

func AuthMiddleware(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			authHeader := request.Header.Get("Authorization")
			if authHeader == "" {
				response.Error(writer, http.StatusUnauthorized, "UNAUTHORIZED", "Authorization header is required")
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				response.Error(writer, http.StatusUnauthorized, "INVALID_TOKEN_FORMAT", "Authorization header must be Bearer <token>")
				return
			}

			tokenString := parts[1]
			claims, err := ValidateToken(tokenString, secret, TokenTypeAccess)
			if err != nil {
				if errors.Is(err, ErrExpiredToken) {
					response.Error(writer, http.StatusUnauthorized, "TOKEN_EXPIRED", "Access token has expired")
					return
				}
				response.Error(writer, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid or malformed access token")
				return
			}

			ctx := context.WithValue(request.Context(), userCtxKey{}, claims)
			next.ServeHTTP(writer, request.WithContext(ctx))
		})
	}
}

// IsOriginAllowed validates whether a browser Origin header is permitted to access the backend.
// It allows explicitly configured origins (single or comma-separated), local loopback ports,
// and same-host origins where the Origin hostname matches the incoming HTTP Host header.
func IsOriginAllowed(originHeader string, requestHost string, allowedOrigins ...string) bool {
	normalizedOrigin := strings.TrimRight(strings.TrimSpace(originHeader), "/")
	if normalizedOrigin == "" {
		return false
	}

	parsedOrigin, parseErr := url.Parse(normalizedOrigin)
	if parseErr != nil || (parsedOrigin.Scheme != "http" && parsedOrigin.Scheme != "https") || parsedOrigin.Host == "" {
		return false
	}

	for _, allowedEntry := range allowedOrigins {
		for _, rawOrigin := range strings.Split(allowedEntry, ",") {
			configuredOrigin := strings.TrimRight(strings.TrimSpace(rawOrigin), "/")
			if configuredOrigin == "" {
				continue
			}
			if configuredOrigin == "*" || strings.EqualFold(normalizedOrigin, configuredOrigin) {
				return true
			}
		}
	}

	originHostname := strings.ToLower(parsedOrigin.Hostname())
	if originHostname == "localhost" || originHostname == "127.0.0.1" || originHostname == "::1" {
		return true
	}

	requestHostname := extractRequestHostname(requestHost)
	if requestHostname != "" && strings.EqualFold(originHostname, requestHostname) {
		return true
	}

	return false
}

func extractRequestHostname(hostPort string) string {
	trimmedHostPort := strings.TrimSpace(hostPort)
	if trimmedHostPort == "" {
		return ""
	}
	if parsedHost, _, splitErr := net.SplitHostPort(trimmedHostPort); splitErr == nil {
		return strings.Trim(parsedHost, "[]")
	}
	return strings.Trim(trimmedHostPort, "[]")
}

func CORSMiddleware(allowedOrigins ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			// Avoid caching issues across different client origins
			writer.Header().Add("Vary", "Origin")

			originHeader := strings.TrimRight(strings.TrimSpace(request.Header.Get("Origin")), "/")
			if originHeader != "" && IsOriginAllowed(originHeader, request.Host, allowedOrigins...) {
				writer.Header().Set("Access-Control-Allow-Origin", originHeader)
			}

			writer.Header().Set("Access-Control-Allow-Credentials", "true")
			writer.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, Cookie")
			writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")

			if request.Method == http.MethodOptions {
				writer.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(writer, request)
		})
	}
}

func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("Unhandled panic in HTTP request",
					"error", rec,
					"path", request.URL.Path,
					"method", request.Method,
				)
				response.Error(writer, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "An unexpected error occurred")
			}
		}()
		next.ServeHTTP(writer, request)
	})
}
