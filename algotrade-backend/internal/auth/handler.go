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

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*10)) // Max 10KB

	if err := decoder.Decode(&req); err != nil {
		
		if maxBytesErr, ok := errors.AsType[*http.MaxBytesError](err); ok {
			response.Error(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE",
				fmt.Sprintf("Request body exceeded maximum limit of %d bytes", maxBytesErr.Limit))
			return
		}
		if syntaxErr, ok := errors.AsType[*json.SyntaxError](err); ok {
			response.Error(w, http.StatusBadRequest, "MALFORMED_JSON",
				fmt.Sprintf("JSON syntax error at character offset %d", syntaxErr.Offset))
			return
		}
		if typeErr, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
			response.Error(w, http.StatusBadRequest, "INVALID_FIELD_TYPE",
				fmt.Sprintf("Field %q expects %s, got %s", typeErr.Field, typeErr.Type, typeErr.Value))
			return
		}
		response.Error(w, http.StatusBadRequest, "INVALID_REQUEST_BODY", "Invalid JSON request body")
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.Password = strings.TrimSpace(req.Password)

	if req.Email == "" || req.Password == "" {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "Email and password are required")
		return
	}

	// Authenticate against decoupled user store (zero credentials in source code)
	authenticatedUser, err := h.store.Authenticate(req.Email, req.Password)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Invalid email or password")
		return
	}

	// Generate 5-min access token and 24-hr refresh token with cryptographic JTI
	accessToken, refreshToken, refreshJTI, err := GenerateTokens(
		authenticatedUser.ID,
		authenticatedUser.Email,
		authenticatedUser.Role,
		h.cfg.JWTSecret,
		h.cfg.AccessTokenExpiry,
		h.cfg.RefreshTokenExpiry,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "TOKEN_GENERATION_FAILED", "Failed to generate tokens")
		return
	}

	// Store active refresh JTI for rotation tracking
	h.activeRefreshJTIs.Store(refreshJTI, time.Now().Add(h.cfg.RefreshTokenExpiry))

	// Set HttpOnly 24-hour cookie for Refresh Token
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    refreshToken,
		Path:     "/api/auth",
		Expires:  time.Now().Add(h.cfg.RefreshTokenExpiry),
		MaxAge:   int(h.cfg.RefreshTokenExpiry.Seconds()),
		HttpOnly: true,
		Secure:   h.cfg.IsProduction,
		SameSite: http.SameSiteLaxMode,
	})

	response.Success(w, http.StatusOK, LoginResponseData{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int(h.cfg.AccessTokenExpiry.Seconds()),
		User: UserInfo{
			ID:    authenticatedUser.ID,
			Email: authenticatedUser.Email,
			Name:  authenticatedUser.Name,
			Role:  authenticatedUser.Role,
		},
	})
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	// Read refresh token from HttpOnly cookie
	cookie, err := r.Cookie("refresh_token")
	if err != nil || cookie.Value == "" {
		response.Error(w, http.StatusUnauthorized, "REFRESH_TOKEN_MISSING", "Refresh token is missing")
		return
	}

	claims, err := ValidateToken(cookie.Value, h.cfg.JWTSecret, TokenTypeRefresh)
	if err != nil {
		if errors.Is(err, ErrExpiredToken) {
			response.Error(w, http.StatusUnauthorized, "REFRESH_TOKEN_EXPIRED", "Refresh token has expired, please log in again")
			return
		}
		response.Error(w, http.StatusUnauthorized, "REFRESH_TOKEN_INVALID", "Invalid refresh token")
		return
	}

	// Verify and atomically consume the JTI for Refresh Token Rotation (RTR)
	if claims.ID == "" {
		response.Error(w, http.StatusUnauthorized, "REFRESH_TOKEN_INVALID", "Refresh token missing token ID")
		return
	}

	_, exists := h.activeRefreshJTIs.LoadAndDelete(claims.ID)
	if !exists {
		// Token replay attack or already-consumed token detected
		response.Error(w, http.StatusUnauthorized, "REFRESH_TOKEN_REVOKED", "Refresh token has been revoked or already used. Please log in again.")
		return
	}

	// Rotate: Issue a fresh 5-minute access token AND a new single-use 24-hour refresh token
	newAccessToken, newRefreshToken, newJTI, err := GenerateTokens(
		claims.UserID,
		claims.Email,
		claims.Role,
		h.cfg.JWTSecret,
		h.cfg.AccessTokenExpiry,
		h.cfg.RefreshTokenExpiry,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "TOKEN_GENERATION_FAILED", "Failed to refresh tokens")
		return
	}

	// Register newly rotated JTI
	h.activeRefreshJTIs.Store(newJTI, time.Now().Add(h.cfg.RefreshTokenExpiry))

	// Update HttpOnly cookie with the newly rotated refresh token
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    newRefreshToken,
		Path:     "/api/auth",
		Expires:  time.Now().Add(h.cfg.RefreshTokenExpiry),
		MaxAge:   int(h.cfg.RefreshTokenExpiry.Seconds()),
		HttpOnly: true,
		Secure:   h.cfg.IsProduction,
		SameSite: http.SameSiteLaxMode,
	})

	response.Success(w, http.StatusOK, RefreshResponseData{
		AccessToken: newAccessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int(h.cfg.AccessTokenExpiry.Seconds()),
	})
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	// Revoke active JTI from registry if present
	if cookie, err := r.Cookie("refresh_token"); err == nil && cookie.Value != "" {
		if claims, err := ValidateToken(cookie.Value, h.cfg.JWTSecret, TokenTypeRefresh); err == nil && claims.ID != "" {
			h.activeRefreshJTIs.Delete(claims.ID)
		}
	}

	// Invalidate HttpOnly cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     "/api/auth",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cfg.IsProduction,
		SameSite: http.SameSiteLaxMode,
	})

	response.Message(w, http.StatusOK, "Successfully logged out")
}
