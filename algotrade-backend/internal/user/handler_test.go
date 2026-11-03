package user

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/auth"
)

type mockStore struct {
	user *User
}

func (store *mockStore) Authenticate(email, password string) (*auth.User, error) {
	return nil, nil
}

func (store *mockStore) GetByID(id string) (*User, error) {
	if store.user != nil && store.user.ID == id {
		return store.user, nil
	}
	return nil, ErrUserNotFound
}

func TestGetProfile_Success(testingInstance *testing.T) {
	store := &mockStore{
		user: &User{
			ID:    "user-123",
			Email: "trader@algotrade.com",
			Name:  "Test Trader",
			Role:  "ADMIN",
		},
	}

	secret := []byte("secret-key-at-least-32-bytes-long-123456")
	accessToken, _, _, err := auth.GenerateTokens("user-123", "trader@algotrade.com", "ADMIN", secret, 5*time.Minute, 24*time.Hour)
	if err != nil {
		testingInstance.Fatalf("failed to generate token: %v", err)
	}

	handler := NewHandler(store)
	middleware := auth.AuthMiddleware(secret)

	req := httptest.NewRequest(http.MethodGet, "/api/user/me", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	rec := httptest.NewRecorder()
	middleware(http.HandlerFunc(handler.GetProfile)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		testingInstance.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp struct {
		IsSuccess bool    `json:"success"`
		Data      Profile `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		testingInstance.Fatalf("failed to decode response: %v", err)
	}

	if !resp.IsSuccess {
		testingInstance.Fatalf("expected success to be true")
	}
	if resp.Data.ID != "user-123" || resp.Data.Name != "Test Trader" || resp.Data.Role != "ADMIN" {
		testingInstance.Fatalf("unexpected profile data: %+v", resp.Data)
	}
}

func TestGetProfile_Unauthorized(testingInstance *testing.T) {
	handler := NewHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/user/me", nil)
	rec := httptest.NewRecorder()

	handler.GetProfile(rec, req)

	if rec.Code != http.StatusUnauthorized {
		testingInstance.Fatalf("expected status 401, got %d", rec.Code)
	}
}
