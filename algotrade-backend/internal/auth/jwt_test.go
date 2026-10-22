package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/config"
)

type mockUserStore struct {
	email    string
	password string
	user     *User
}

func (m *mockUserStore) Authenticate(email, password string) (*User, error) {
	if email == m.email && password == m.password {
		return m.user, nil
	}
	return nil, errors.New("invalid credentials")
}

func TestGenerateAndValidateTokens(t *testing.T) {
	secret := []byte("test-secret-key-12345678901234567890")
	userID := "usr_1001"
	email := "trader@algotrade.com"
	role := "PRO_TRADER"

	// 1. Generate valid tokens with JTI
	accessToken, refreshToken, refreshJTI, err := GenerateTokens(userID, email, role, secret, 5*time.Minute, 24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateTokens failed: %v", err)
	}
	if accessToken == "" || refreshToken == "" || refreshJTI == "" {
		t.Fatalf("Tokens and JTI should not be empty")
	}

	// 2. Validate Access Token as access type
	accessClaims, err := ValidateToken(accessToken, secret, TokenTypeAccess)
	if err != nil {
		t.Fatalf("Validate access token failed: %v", err)
	}
	if accessClaims.UserID != userID || accessClaims.Email != email || accessClaims.Role != role {
		t.Errorf("Claims mismatch: got %+v", accessClaims)
	}

	// 3. Ensure Access Token cannot be used as Refresh Token
	_, err = ValidateToken(accessToken, secret, TokenTypeRefresh)
	if err != ErrWrongTokenType {
		t.Errorf("Expected ErrWrongTokenType, got %v", err)
	}

	// 4. Validate Refresh Token has matching JTI
	refreshClaims, err := ValidateToken(refreshToken, secret, TokenTypeRefresh)
	if err != nil {
		t.Fatalf("Validate refresh token failed: %v", err)
	}
	if refreshClaims.UserID != userID {
		t.Errorf("Refresh claims mismatch: got %+v", refreshClaims)
	}
	if refreshClaims.ID != refreshJTI {
		t.Errorf("Expected refresh claims ID to match JTI %s, got %s", refreshJTI, refreshClaims.ID)
	}

	// 5. Test expired token
	expiredAccessToken, _, _, err := GenerateTokens(userID, email, role, secret, -1*time.Minute, 24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateTokens failed: %v", err)
	}
	_, err = ValidateToken(expiredAccessToken, secret, TokenTypeAccess)
	if err != ErrExpiredToken {
		t.Errorf("Expected ErrExpiredToken, got %v", err)
	}
}

func TestRefreshTokenRotationAndReplay(t *testing.T) {
	cfg := &config.Config{
		JWTSecret:          []byte("test-secret-123456789012345678901234"),
		AccessTokenExpiry:  5 * time.Minute,
		RefreshTokenExpiry: 24 * time.Hour,
	}

	mockStore := &mockUserStore{
		email:    "trader@algotrade.com",
		password: "Password123!",
		user: &User{
			ID:    "usr_test",
			Email: "trader@algotrade.com",
			Name:  "Test Trader",
			Role:  "PRO_TRADER",
		},
	}

	handler := NewHandler(cfg, mockStore)

	// Step 1: Login
	loginBody, _ := json.Marshal(map[string]string{
		"email":    "trader@algotrade.com",
		"password": "Password123!",
	})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))
	loginRec := httptest.NewRecorder()
	handler.Login(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("Login failed with status: %d", loginRec.Code)
	}

	cookies := loginRec.Result().Cookies()
	var initialRefreshCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "refresh_token" {
			initialRefreshCookie = c
			break
		}
	}
	if initialRefreshCookie == nil {
		t.Fatalf("Expected refresh_token cookie on login")
	}

	// Step 2: First Refresh (Valid)
	refreshReq1 := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	refreshReq1.AddCookie(initialRefreshCookie)
	refreshRec1 := httptest.NewRecorder()
	handler.Refresh(refreshRec1, refreshReq1)

	if refreshRec1.Code != http.StatusOK {
		t.Fatalf("First refresh failed with status: %d, body: %s", refreshRec1.Code, refreshRec1.Body.String())
	}

	var rotatedRefreshCookie *http.Cookie
	for _, c := range refreshRec1.Result().Cookies() {
		if c.Name == "refresh_token" {
			rotatedRefreshCookie = c
			break
		}
	}
	if rotatedRefreshCookie == nil {
		t.Fatalf("Expected rotated refresh_token cookie on refresh")
	}
	if rotatedRefreshCookie.Value == initialRefreshCookie.Value {
		t.Fatalf("Rotated refresh token must not match initial refresh token")
	}

	// Step 3: Replay Attack (Reusing initial consumed refresh token MUST FAIL)
	replayReq := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	replayReq.AddCookie(initialRefreshCookie)
	replayRec := httptest.NewRecorder()
	handler.Refresh(replayRec, replayReq)

	if replayRec.Code != http.StatusUnauthorized {
		t.Fatalf("Expected replay of consumed token to return 401, got %d", replayRec.Code)
	}

	// Step 4: Valid Refresh with the newly rotated cookie MUST SUCCEED
	refreshReq2 := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	refreshReq2.AddCookie(rotatedRefreshCookie)
	refreshRec2 := httptest.NewRecorder()
	handler.Refresh(refreshRec2, refreshReq2)

	if refreshRec2.Code != http.StatusOK {
		t.Fatalf("Refresh with rotated cookie failed with status: %d", refreshRec2.Code)
	}
}
