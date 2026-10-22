package config

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port               string
	JWTSecret          []byte
	FrontendURL        string
	Env                string
	AccessTokenExpiry  time.Duration
	RefreshTokenExpiry time.Duration
	IsProduction       bool

	// Initial authenticated user configuration (loaded from .env, zero hardcoded in code)
	AuthUserEmail    string
	AuthUserPassword string
	AuthUserName     string
	AuthUserID       string
	AuthUserRole     string
}

type ConfigValidationError struct {
	MissingFields []string
	InvalidFields map[string]string
}

func (e *ConfigValidationError) Error() string {
	var sb strings.Builder
	sb.WriteString("configuration validation failed:\n")
	if len(e.MissingFields) > 0 {
		sb.WriteString(fmt.Sprintf("  - missing required variables: %s\n", strings.Join(e.MissingFields, ", ")))
	}
	for k, v := range e.InvalidFields {
		sb.WriteString(fmt.Sprintf("  - invalid variable %s: %s\n", k, v))
	}
	return sb.String()
}

// loadEnvFile reads a .env file and sets variables that are not already present in the environment
func loadEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // No .env file is fine; OS environment variables will be used
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comment lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		// Strip quotes if present ("value" or 'value')
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}

		// Only set if not already set in OS environment (12-factor standard)
		if os.Getenv(key) == "" {
			_ = os.Setenv(key, val)
		}
	}

	return scanner.Err()
}

// LoadAndValidate loads environment variables from .env and strictly validates all required variables
func LoadAndValidate() (*Config, error) {
	// Attempt to load .env file if present
	_ = loadEnvFile(".env")

	missing := make([]string, 0)
	invalid := make(map[string]string)

	getRequired := func(key string) string {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			missing = append(missing, key)
		}
		return v
	}

	portStr := getRequired("PORT")
	if portStr != "" {
		portNum, err := strconv.Atoi(portStr)
		if err != nil || portNum < 1024 || portNum > 65535 {
			invalid["PORT"] = "must be a valid port number between 1024 and 65535"
		}
	}

	envStr := getRequired("ENV")
	if envStr != "" {
		envLower := strings.ToLower(envStr)
		if envLower != "development" && envLower != "production" && envLower != "test" {
			invalid["ENV"] = "must be 'development', 'production', or 'test'"
		}
	}

	jwtSecretStr := getRequired("JWT_SECRET")
	if jwtSecretStr != "" && len(jwtSecretStr) < 32 {
		invalid["JWT_SECRET"] = "must be at least 32 characters long for HMAC-SHA256 cryptographic security"
	}

	frontendURL := getRequired("FRONTEND_URL")
	if frontendURL != "" {
		u, err := url.Parse(frontendURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			invalid["FRONTEND_URL"] = "must be a valid URL with scheme (e.g. http://localhost:3000)"
		} else {
			frontendURL = strings.TrimRight(frontendURL, "/")
		}
	}

	authUserEmail := getRequired("AUTH_USER_EMAIL")
	if authUserEmail != "" {
		if !strings.Contains(authUserEmail, "@") || !strings.Contains(authUserEmail, ".") {
			invalid["AUTH_USER_EMAIL"] = "must be a valid email address"
		}
	}

	authUserPassword := getRequired("AUTH_USER_PASSWORD")
	if authUserPassword != "" && len(authUserPassword) < 8 {
		invalid["AUTH_USER_PASSWORD"] = "must be at least 8 characters long"
	}

	authUserName := getRequired("AUTH_USER_NAME")
	authUserID := getRequired("AUTH_USER_ID")
	authUserRole := getRequired("AUTH_USER_ROLE")

	if len(missing) > 0 || len(invalid) > 0 {
		return nil, &ConfigValidationError{
			MissingFields: missing,
			InvalidFields: invalid,
		}
	}

	isProd := strings.EqualFold(envStr, "production")

	return &Config{
		Port:               portStr,
		JWTSecret:          []byte(jwtSecretStr),
		FrontendURL:        frontendURL,
		Env:                envStr,
		AccessTokenExpiry:  5 * time.Minute,
		RefreshTokenExpiry: 24 * time.Hour,
		IsProduction:       isProd,
		AuthUserEmail:      authUserEmail,
		AuthUserPassword:   authUserPassword,
		AuthUserName:       authUserName,
		AuthUserID:         authUserID,
		AuthUserRole:       authUserRole,
	}, nil
}
