package config

import (
	"errors"
	"os"
	"testing"
)

func TestConfigValidation_MissingVariables(t *testing.T) {
	os.Setenv("SKIP_ENV_LOAD", "true")
	defer os.Unsetenv("SKIP_ENV_LOAD")

	// Clear all relevant environment variables
	envKeys := []string{
		"PORT", "ENV", "JWT_SECRET", "FRONTEND_URL",
		"AUTH_USER_EMAIL", "AUTH_USER_PASSWORD", "AUTH_USER_NAME",
		"AUTH_USER_ID", "AUTH_USER_ROLE",
	}
	for _, k := range envKeys {
		os.Unsetenv(k)
	}

	_, err := LoadAndValidate()
	if err == nil {
		t.Fatalf("Expected LoadAndValidate to fail on empty environment, but it succeeded")
	}

	// Use modern Go 1.24 errors.AsType
	valErr, ok := errors.AsType[*ConfigValidationError](err)
	if !ok {
		t.Fatalf("Expected error of type *ConfigValidationError, got %T: %v", err, err)
	}

	if len(valErr.MissingFields) == 0 {
		t.Errorf("Expected MissingFields to be populated, got empty list")
	}
}

func TestConfigValidation_InvalidSecretAndPort(t *testing.T) {
	os.Setenv("SKIP_ENV_LOAD", "true")
	defer os.Unsetenv("SKIP_ENV_LOAD")

	os.Setenv("PORT", "99999") // invalid port > 65535
	os.Setenv("ENV", "development")
	os.Setenv("JWT_SECRET", "short-secret") // < 32 chars
	os.Setenv("FRONTEND_URL", "http://localhost:3000")
	os.Setenv("AUTH_USER_EMAIL", "test@test.com")
	os.Setenv("AUTH_USER_PASSWORD", "Secret123!")
	os.Setenv("AUTH_USER_NAME", "Tester")
	os.Setenv("AUTH_USER_ID", "usr_1")
	os.Setenv("AUTH_USER_ROLE", "TRADER")

	_, err := LoadAndValidate()
	if err == nil {
		t.Fatalf("Expected validation error for invalid port and short secret")
	}

	valErr, ok := errors.AsType[*ConfigValidationError](err)
	if !ok {
		t.Fatalf("Expected *ConfigValidationError, got %T", err)
	}

	if _, exists := valErr.InvalidFields["PORT"]; !exists {
		t.Errorf("Expected PORT to be flagged in InvalidFields")
	}
	if _, exists := valErr.InvalidFields["JWT_SECRET"]; !exists {
		t.Errorf("Expected JWT_SECRET to be flagged in InvalidFields")
	}
}
