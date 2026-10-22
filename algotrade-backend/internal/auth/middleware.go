package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
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
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "Authorization header is required")
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				response.Error(w, http.StatusUnauthorized, "INVALID_TOKEN_FORMAT", "Authorization header must be Bearer <token>")
				return
			}

			tokenString := parts[1]
			claims, err := ValidateToken(tokenString, secret, TokenTypeAccess)
			if err != nil {
				if errors.Is(err, ErrExpiredToken) {
					response.Error(w, http.StatusUnauthorized, "TOKEN_EXPIRED", "Access token has expired")
					return
				}
				response.Error(w, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid or malformed access token")
				return
			}

			ctx := context.WithValue(r.Context(), userCtxKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func CORSMiddleware(frontendURL string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Avoid caching issues across different client origins
			w.Header().Add("Vary", "Origin")

			origin := r.Header.Get("Origin")
			if origin == frontendURL || frontendURL == "*" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			} else if origin != "" {
				// Allow localhost dynamically if testing with different ports
				if strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:") {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				}
			}

			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, Cookie")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("Unhandled panic in HTTP request",
					"error", rec,
					"path", r.URL.Path,
					"method", r.Method,
				)
				response.Error(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "An unexpected error occurred")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
