package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigValidation_MissingVariables(testingInstance *testing.T) {
	os.Setenv("SKIP_ENV_LOAD", "true")
	defer os.Unsetenv("SKIP_ENV_LOAD")

	// Clear all relevant environment variables
	envKeys := []string{
		"PORT", "ENV", "JWT_SECRET", "FRONTEND_URL",
		"AUTH_USER_EMAIL", "AUTH_USER_PASSWORD", "AUTH_USER_NAME",
		"AUTH_USER_ID", "AUTH_USER_ROLE",
	}
	for _, envKey := range envKeys {
		os.Unsetenv(envKey)
	}

	_, err := LoadAndValidate()
	if err == nil {
		testingInstance.Fatalf("Expected LoadAndValidate to fail on empty environment, but it succeeded")
	}

	// Use modern Go 1.24 errors.AsType
	valErr, ok := errors.AsType[*ConfigValidationError](err)
	if !ok {
		testingInstance.Fatalf("Expected error of type *ConfigValidationError, got %T: %v", err, err)
	}

	if len(valErr.MissingFields) == 0 {
		testingInstance.Errorf("Expected MissingFields to be populated, got empty list")
	}
}

func TestConfigValidation_InvalidSecretAndPort(testingInstance *testing.T) {
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
		testingInstance.Fatalf("Expected validation error for invalid port and short secret")
	}

	valErr, ok := errors.AsType[*ConfigValidationError](err)
	if !ok {
		testingInstance.Fatalf("Expected *ConfigValidationError, got %T", err)
	}

	if _, exists := valErr.InvalidFields["PORT"]; !exists {
		testingInstance.Errorf("Expected PORT to be flagged in InvalidFields")
	}
	if _, exists := valErr.InvalidFields["JWT_SECRET"]; !exists {
		testingInstance.Errorf("Expected JWT_SECRET to be flagged in InvalidFields")
	}
}

func TestPhase4_MultiOriginConfigAndEnvFilePermissions(testingInstance *testing.T) {
	os.Setenv("SKIP_ENV_LOAD", "true")
	defer os.Unsetenv("SKIP_ENV_LOAD")

	os.Setenv("PORT", "8080")
	os.Setenv("ENV", "production")
	os.Setenv("JWT_SECRET", "super-secure-production-jwt-secret-key-32chars!")
	os.Setenv("FRONTEND_URL", "http://localhost:3000/, http://52.66.202.136:3000/")
	os.Setenv("AUTH_USER_EMAIL", "trader@algotrade.com")
	os.Setenv("AUTH_USER_PASSWORD", "Password123!")
	os.Setenv("AUTH_USER_NAME", "Quant Trader")
	os.Setenv("AUTH_USER_ID", "usr_quant_1")
	os.Setenv("AUTH_USER_ROLE", "PRO_TRADER")

	loadedConfig, validationErr := LoadAndValidate()
	if validationErr != nil {
		testingInstance.Fatalf("Expected multi-origin FRONTEND_URL to validate cleanly, got: %v", validationErr)
	}

	if loadedConfig.FrontendURL != "http://localhost:3000" {
		testingInstance.Fatalf("Expected primary FrontendURL to be http://localhost:3000, got %s", loadedConfig.FrontendURL)
	}
	if len(loadedConfig.AllowedOrigins) != 2 ||
		loadedConfig.AllowedOrigins[0] != "http://localhost:3000" ||
		loadedConfig.AllowedOrigins[1] != "http://52.66.202.136:3000" {
		testingInstance.Fatalf("Unexpected AllowedOrigins slice: %+v", loadedConfig.AllowedOrigins)
	}

	// Verify dynamic frontend origin resolution for remote Host header even when only localhost is configured
	singleOriginConfig := &Config{
		FrontendURL:    "http://localhost:3000",
		AllowedOrigins: []string{"http://localhost:3000"},
	}
	resolvedRemoteURL := singleOriginConfig.ResolveFrontendURLForRequestHost("52.66.202.136:8080")
	if resolvedRemoteURL != "http://52.66.202.136:3000" {
		testingInstance.Fatalf("Expected ResolveFrontendURLForRequestHost to resolve http://52.66.202.136:3000, got %s", resolvedRemoteURL)
	}
	resolvedLocalURL := singleOriginConfig.ResolveFrontendURLForRequestHost("localhost:8080")
	if resolvedLocalURL != "http://localhost:3000" {
		testingInstance.Fatalf("Expected ResolveFrontendURLForRequestHost to preserve http://localhost:3000, got %s", resolvedLocalURL)
	}

	// Verify automatic 0600 permission tightening on world-readable .env file
	tempDirectory := testingInstance.TempDir()
	tempEnvPath := filepath.Join(tempDirectory, ".env")
	if writeErr := os.WriteFile(tempEnvPath, []byte("TEST_PHASE4_ENV_KEY=locked_value\n"), 0o644); writeErr != nil {
		testingInstance.Fatalf("Failed to create temporary .env file: %v", writeErr)
	}
	if chmodErr := os.Chmod(tempEnvPath, 0o644); chmodErr != nil {
		testingInstance.Fatalf("Failed to set initial 0644 mode on temporary .env file: %v", chmodErr)
	}

	if loadErr := loadEnvFile(tempEnvPath); loadErr != nil {
		testingInstance.Fatalf("loadEnvFile failed: %v", loadErr)
	}
	defer os.Unsetenv("TEST_PHASE4_ENV_KEY")

	fileInfo, statErr := os.Stat(tempEnvPath)
	if statErr != nil {
		testingInstance.Fatalf("Failed to stat temporary .env file: %v", statErr)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		testingInstance.Fatalf("Expected loadEnvFile to tighten .env permissions to 0600, got %04o", fileInfo.Mode().Perm())
	}
}
