package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrExpiredToken   = errors.New("token has expired")
	ErrInvalidToken   = errors.New("token is invalid")
	ErrWrongTokenType = errors.New("token type mismatch")
)

const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

type CustomClaims struct {
	UserID    string `json:"user_id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	TokenType string `json:"token_type"`
	jwt.RegisteredClaims
}

func generateSecureID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// GenerateTokens generates a 5-minute access token and a 24-hour refresh token with a unique cryptographic JTI
func GenerateTokens(userID, email, role string, secret []byte, accessTTL, refreshTTL time.Duration) (string, string, string, error) {
	now := time.Now()

	// 5-minute access token
	accessClaims := CustomClaims{
		UserID:    userID,
		Email:     email,
		Role:      role,
		TokenType: TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(accessTTL)),
		},
	}
	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(secret)
	if err != nil {
		return "", "", "", err
	}

	// Unique cryptographic token ID (JTI) for single-use Refresh Token Rotation
	refreshJTI, err := generateSecureID()
	if err != nil {
		return "", "", "", err
	}

	// 24-hour refresh token
	refreshClaims := CustomClaims{
		UserID:    userID,
		Email:     email,
		Role:      role,
		TokenType: TokenTypeRefresh,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        refreshJTI,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(refreshTTL)),
		},
	}
	refreshToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString(secret)
	if err != nil {
		return "", "", "", err
	}

	return accessToken, refreshToken, refreshJTI, nil
}

func ValidateToken(tokenString string, secret []byte, expectedType string) (*CustomClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &CustomClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return secret, nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*CustomClaims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	if claims.TokenType != expectedType {
		return nil, ErrWrongTokenType
	}

	return claims, nil
}
