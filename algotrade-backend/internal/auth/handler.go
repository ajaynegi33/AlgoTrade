package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/config"
	"github.com/ajaynegi33/AlgoTrade/internal/response"
)

type User struct {
	ID    string
	Email string
	Name  string
	Role  string
}

type UserStore interface {
	Authenticate(email, password string) (*User, error)
}

type Handler struct {
	cfg   *config.Config
	store UserStore

	// Active single-use refresh token registry for Refresh Token Rotation (RTR)
	// key: jti (string) -> value: expiresAt (time.Time)
	activeRefreshJTIs sync.Map
}

func NewHandler(cfg *config.Config, store UserStore) *Handler {
	return &Handler{
		cfg:   cfg,
		store: store,
	}
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type UserInfo struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

type LoginResponseData struct {
	AccessToken string   `json:"access_token"`
	TokenType   string   `json:"token_type"`
	ExpiresIn   int      `json:"expires_in"` // in seconds (300)
	User        UserInfo `json:"user"`
}

type RefreshResponseData struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"` // in seconds (300)
}

func (handler *Handler) Login(writer http.ResponseWriter, request *http.Request) {
	var loginRequest LoginRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1024*10)) // Max 10KB

	if err := decoder.Decode(&loginRequest); err != nil {
		
		if maxBytesErr, ok := errors.AsType[*http.MaxBytesError](err); ok {
			response.Error(writer, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE",
				fmt.Sprintf("Request body exceeded maximum limit of %d bytes", maxBytesErr.Limit))
			return
		}
		if syntaxErr, ok := errors.AsType[*json.SyntaxError](err); ok {
			response.Error(writer, http.StatusBadRequest, "MALFORMED_JSON",
				fmt.Sprintf("JSON syntax error at character offset %d", syntaxErr.Offset))
			return
		}
		if typeErr, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
			response.Error(writer, http.StatusBadRequest, "INVALID_FIELD_TYPE",
				fmt.Sprintf("Field %q expects %s, got %s", typeErr.Field, typeErr.Type, typeErr.Value))
			return
		}
		response.Error(writer, http.StatusBadRequest, "INVALID_REQUEST_BODY", "Invalid JSON request body")
		return
	}

	loginRequest.Email = strings.TrimSpace(strings.ToLower(loginRequest.Email))
	loginRequest.Password = strings.TrimSpace(loginRequest.Password)

	if loginRequest.Email == "" || loginRequest.Password == "" {
		response.Error(writer, http.StatusBadRequest, "VALIDATION_ERROR", "Email and password are required")
		return
	}

	// Authenticate against decoupled user store (zero credentials in source code)
	authenticatedUser, err := handler.store.Authenticate(loginRequest.Email, loginRequest.Password)
	if err != nil {
		response.Error(writer, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Invalid email or password")
		return
	}

	// Generate 5-min access token and 24-hr refresh token with cryptographic JTI
	accessToken, refreshToken, refreshJTI, err := GenerateTokens(
		authenticatedUser.ID,
		authenticatedUser.Email,
		authenticatedUser.Role,
		handler.cfg.JWTSecret,
		handler.cfg.AccessTokenExpiry,
		handler.cfg.RefreshTokenExpiry,
	)
	if err != nil {
		response.Error(writer, http.StatusInternalServerError, "TOKEN_GENERATION_FAILED", "Failed to generate tokens")
		return
	}

	// Store active refresh JTI for rotation tracking
	handler.activeRefreshJTIs.Store(refreshJTI, time.Now().Add(handler.cfg.RefreshTokenExpiry))

	// Set HttpOnly 24-hour cookie for Refresh Token
	http.SetCookie(writer, &http.Cookie{
		Name:     "refresh_token",
		Value:    refreshToken,
		Path:     "/api/auth",
		Expires:  time.Now().Add(handler.cfg.RefreshTokenExpiry),
		MaxAge:   int(handler.cfg.RefreshTokenExpiry.Seconds()),
		HttpOnly: true,
		Secure:   shouldUseSecureCookie(request, handler.cfg),
		SameSite: http.SameSiteLaxMode,
	})

	response.Success(writer, http.StatusOK, LoginResponseData{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int(handler.cfg.AccessTokenExpiry.Seconds()),
		User: UserInfo{
			ID:    authenticatedUser.ID,
			Email: authenticatedUser.Email,
			Name:  authenticatedUser.Name,
			Role:  authenticatedUser.Role,
		},
	})
}

func (handler *Handler) Refresh(writer http.ResponseWriter, request *http.Request) {
	// Read refresh token from HttpOnly cookie
	refreshTokenCookie, err := request.Cookie("refresh_token")
	if err != nil || refreshTokenCookie.Value == "" {
		response.Error(writer, http.StatusUnauthorized, "REFRESH_TOKEN_MISSING", "Refresh token is missing")
		return
	}

	claims, err := ValidateToken(refreshTokenCookie.Value, handler.cfg.JWTSecret, TokenTypeRefresh)
	if err != nil {
		if errors.Is(err, ErrExpiredToken) {
			response.Error(writer, http.StatusUnauthorized, "REFRESH_TOKEN_EXPIRED", "Refresh token has expired, please log in again")
			return
		}
		response.Error(writer, http.StatusUnauthorized, "REFRESH_TOKEN_INVALID", "Invalid refresh token")
		return
	}

	// Verify and atomically consume the JTI for Refresh Token Rotation (RTR)
	if claims.ID == "" {
		response.Error(writer, http.StatusUnauthorized, "REFRESH_TOKEN_INVALID", "Refresh token missing token ID")
		return
	}

	_, hasTokenRecord := handler.activeRefreshJTIs.LoadAndDelete(claims.ID)
	if !hasTokenRecord {
		// Token replay attack or already-consumed token detected
		response.Error(writer, http.StatusUnauthorized, "REFRESH_TOKEN_REVOKED", "Refresh token has been revoked or already used. Please log in again.")
		return
	}

	// Rotate: Issue a fresh 5-minute access token AND a new single-use 24-hour refresh token
	newAccessToken, newRefreshToken, newJTI, err := GenerateTokens(
		claims.UserID,
		claims.Email,
		claims.Role,
		handler.cfg.JWTSecret,
		handler.cfg.AccessTokenExpiry,
		handler.cfg.RefreshTokenExpiry,
	)
	if err != nil {
		response.Error(writer, http.StatusInternalServerError, "TOKEN_GENERATION_FAILED", "Failed to refresh tokens")
		return
	}

	// Register newly rotated JTI
	handler.activeRefreshJTIs.Store(newJTI, time.Now().Add(handler.cfg.RefreshTokenExpiry))

	// Update HttpOnly cookie with the newly rotated refresh token
	http.SetCookie(writer, &http.Cookie{
		Name:     "refresh_token",
		Value:    newRefreshToken,
		Path:     "/api/auth",
		Expires:  time.Now().Add(handler.cfg.RefreshTokenExpiry),
		MaxAge:   int(handler.cfg.RefreshTokenExpiry.Seconds()),
		HttpOnly: true,
		Secure:   shouldUseSecureCookie(request, handler.cfg),
		SameSite: http.SameSiteLaxMode,
	})

	response.Success(writer, http.StatusOK, RefreshResponseData{
		AccessToken: newAccessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int(handler.cfg.AccessTokenExpiry.Seconds()),
	})
}

func (handler *Handler) Logout(writer http.ResponseWriter, request *http.Request) {
	// Revoke active JTI from registry if present
	if refreshTokenCookie, err := request.Cookie("refresh_token"); err == nil && refreshTokenCookie.Value != "" {
		if claims, err := ValidateToken(refreshTokenCookie.Value, handler.cfg.JWTSecret, TokenTypeRefresh); err == nil && claims.ID != "" {
			handler.activeRefreshJTIs.Delete(claims.ID)
		}
	}

	// Invalidate HttpOnly cookie
	http.SetCookie(writer, &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     "/api/auth",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   shouldUseSecureCookie(request, handler.cfg),
		SameSite: http.SameSiteLaxMode,
	})

	response.Message(writer, http.StatusOK, "Successfully logged out")
}

// shouldUseSecureCookie determines whether the refresh_token cookie should set the Secure attribute.
// Browsers reject Secure cookies over plain HTTP on non-localhost IPs (such as http://52.66.202.136:3000),
// so Secure is enabled only when the transport or origin is HTTPS.
func shouldUseSecureCookie(request *http.Request, cfg *config.Config) bool {
	if request != nil {
		if request.TLS != nil || strings.EqualFold(request.Header.Get("X-Forwarded-Proto"), "https") {
			return true
		}
		if originHeader := strings.TrimSpace(request.Header.Get("Origin")); originHeader != "" {
			return strings.HasPrefix(strings.ToLower(originHeader), "https://")
		}
	}
	return cfg != nil && cfg.IsProduction && strings.HasPrefix(strings.ToLower(cfg.FrontendURL), "https://")
}
