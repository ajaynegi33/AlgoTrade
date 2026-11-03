package config

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port               string
	JWTSecret          []byte
	FrontendURL        string
	AllowedOrigins     []string
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

	// Database configuration
	DatabasePath string

	// Zerodha Kite Connect configuration
	KiteAPIKey      string
	KiteAPISecret   string
	KiteRedirectURL string
}

// IsKiteConfigured returns true if Kite API key and secret are both provided
func (config *Config) IsKiteConfigured() bool {
	return config.KiteAPIKey != "" && config.KiteAPISecret != ""
}

// ResolveFrontendURLForRequestHost returns the best matching frontend origin for the incoming HTTP Host header.
func (config *Config) ResolveFrontendURLForRequestHost(requestHost string) string {
	if config == nil {
		return ""
	}
	requestHostname := extractHostname(requestHost)
	if requestHostname == "" {
		return config.FrontendURL
	}

	for _, allowedOrigin := range config.AllowedOrigins {
		parsedOrigin, parseErr := url.Parse(allowedOrigin)
		if parseErr == nil && strings.EqualFold(parsedOrigin.Hostname(), requestHostname) {
			return allowedOrigin
		}
	}

	// If primary FrontendURL is loopback (localhost/127.0.0.1) but request arrived on a remote IP/host,
	// preserve the frontend scheme and port while targeting the remote hostname.
	primaryParsed, parseErr := url.Parse(config.FrontendURL)
	if parseErr == nil && isLoopbackHost(primaryParsed.Hostname()) && !isLoopbackHost(requestHostname) {
		frontendPort := primaryParsed.Port()
		if frontendPort != "" {
			return fmt.Sprintf("%s://%s:%s", primaryParsed.Scheme, requestHostname, frontendPort)
		}
		return fmt.Sprintf("%s://%s", primaryParsed.Scheme, requestHostname)
	}

	return config.FrontendURL
}

func extractHostname(hostPort string) string {
	trimmedHostPort := strings.TrimSpace(hostPort)
	if trimmedHostPort == "" {
		return ""
	}
	if parsedHost, _, splitErr := net.SplitHostPort(trimmedHostPort); splitErr == nil {
		return strings.Trim(parsedHost, "[]")
	}
	return strings.Trim(trimmedHostPort, "[]")
}

func isLoopbackHost(hostname string) bool {
	normalizedHost := strings.ToLower(strings.TrimSpace(hostname))
	return normalizedHost == "localhost" || normalizedHost == "127.0.0.1" || normalizedHost == "::1"
}

type ConfigValidationError struct {
	MissingFields []string
	InvalidFields map[string]string
}

func (validationError *ConfigValidationError) Error() string {
	var errorStringBuilder strings.Builder
	errorStringBuilder.WriteString("configuration validation failed:\n")
	if len(validationError.MissingFields) > 0 {
		errorStringBuilder.WriteString(fmt.Sprintf("  - missing required variables: %s\n", strings.Join(validationError.MissingFields, ", ")))
	}
	for fieldName, fieldError := range validationError.InvalidFields {
		errorStringBuilder.WriteString(fmt.Sprintf("  - invalid variable %s: %s\n", fieldName, fieldError))
	}
	return errorStringBuilder.String()
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

	if fileInfo, statErr := file.Stat(); statErr == nil && fileInfo.Mode().Perm()&0o077 != 0 {
		if chmodErr := os.Chmod(path, 0o600); chmodErr != nil {
			slog.Warn("Unable to tighten .env file permissions to 0600", "path", path, "error", chmodErr)
		}
	}

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

// findAndLoadEnvFile searches current and parent directories for .env configuration files
func findAndLoadEnvFile(paths ...string) (string, error) {
	if os.Getenv("SKIP_ENV_LOAD") == "true" {
		return "", nil
	}

	if len(paths) > 0 && paths[0] != "" {
		if err := loadEnvFile(paths[0]); err != nil {
			return "", err
		}
		return paths[0], nil
	}

	if envFile := os.Getenv("ENV_FILE"); envFile != "" {
		if err := loadEnvFile(envFile); err == nil {
			return envFile, nil
		}
	}

	candidatePaths := []string{
		".env",
		".env.dev",
		".env.local",
		"../.env",
		"../../.env",
		"algotrade-backend/.env",
	}

	for _, candidatePath := range candidatePaths {
		if fileInfo, err := os.Stat(candidatePath); err == nil && !fileInfo.IsDir() {
			if err := loadEnvFile(candidatePath); err == nil {
				return candidatePath, nil
			}
		}
	}

	// Dynamic walk-up search from current working directory
	cwd, err := os.Getwd()
	if err == nil {
		dir := cwd
		for directoryLevel := 0; directoryLevel < 4; directoryLevel++ {
			testPath := filepath.Join(dir, ".env")
			if fileInfo, err := os.Stat(testPath); err == nil && !fileInfo.IsDir() {
				if err := loadEnvFile(testPath); err == nil {
					return testPath, nil
				}
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	return "", nil
}

// LoadAndValidate loads environment variables from .env and strictly validates all required variables
func LoadAndValidate(paths ...string) (*Config, error) {
	loadedPath, _ := findAndLoadEnvFile(paths...)
	if loadedPath != "" {
		slog.Debug("Loaded environment configuration file", "path", loadedPath)
	}

	missingVariables := make([]string, 0)
	invalidConfigs := make(map[string]string)

	getRequiredVariable := func(variableKey string) string {
		variableValue := strings.TrimSpace(os.Getenv(variableKey))
		if variableValue == "" {
			missingVariables = append(missingVariables, variableKey)
		}
		return variableValue
	}

	portStr := getRequiredVariable("PORT")
	if portStr != "" {
		portNum, err := strconv.Atoi(portStr)
		if err != nil || portNum < 1024 || portNum > 65535 {
			invalidConfigs["PORT"] = "must be a valid port number between 1024 and 65535"
		}
	}

	envStr := getRequiredVariable("ENV")
	if envStr != "" {
		envLower := strings.ToLower(envStr)
		if envLower != "development" && envLower != "production" && envLower != "test" {
			invalidConfigs["ENV"] = "must be 'development', 'production', or 'test'"
		}
	}

	jwtSecretStr := getRequiredVariable("JWT_SECRET")
	if jwtSecretStr != "" && len(jwtSecretStr) < 32 {
		invalidConfigs["JWT_SECRET"] = "must be at least 32 characters long for HMAC-SHA256 cryptographic security"
	}

	frontendURL := getRequiredVariable("FRONTEND_URL")
	allowedOrigins := make([]string, 0)
	if frontendURL != "" {
		rawOriginParts := strings.Split(frontendURL, ",")
		for _, rawOriginPart := range rawOriginParts {
			trimmedOrigin := strings.TrimRight(strings.TrimSpace(rawOriginPart), "/")
			if trimmedOrigin == "" {
				continue
			}
			if trimmedOrigin == "*" {
				allowedOrigins = append(allowedOrigins, "*")
				continue
			}
			parsedURL, parseErr := url.Parse(trimmedOrigin)
			if parseErr != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
				invalidConfigs["FRONTEND_URL"] = "must be one or more valid comma-separated URLs with scheme (e.g. http://localhost:3000)"
				break
			}
			allowedOrigins = append(allowedOrigins, trimmedOrigin)
		}
		if len(allowedOrigins) == 0 && invalidConfigs["FRONTEND_URL"] == "" {
			invalidConfigs["FRONTEND_URL"] = "must be a valid URL with scheme (e.g. http://localhost:3000)"
		} else if len(allowedOrigins) > 0 {
			frontendURL = allowedOrigins[0]
		}
	}

	authUserEmail := getRequiredVariable("AUTH_USER_EMAIL")
	if authUserEmail != "" {
		if !strings.Contains(authUserEmail, "@") || !strings.Contains(authUserEmail, ".") {
			invalidConfigs["AUTH_USER_EMAIL"] = "must be a valid email address"
		}
	}

	authUserPassword := getRequiredVariable("AUTH_USER_PASSWORD")
	if authUserPassword != "" && len(authUserPassword) < 8 {
		invalidConfigs["AUTH_USER_PASSWORD"] = "must be at least 8 characters long"
	}

	authUserName := getRequiredVariable("AUTH_USER_NAME")
	authUserID := getRequiredVariable("AUTH_USER_ID")
	authUserRole := getRequiredVariable("AUTH_USER_ROLE")

	if len(missingVariables) > 0 || len(invalidConfigs) > 0 {
		return nil, &ConfigValidationError{
			MissingFields: missingVariables,
			InvalidFields: invalidConfigs,
		}
	}

	isProduction := strings.EqualFold(envStr, "production")

	dbPath := strings.TrimSpace(os.Getenv("DATABASE_PATH"))
	if dbPath == "" {
		dbPath = "data/algotrade.db"
	}

	kiteAPIKey := strings.TrimSpace(os.Getenv("KITE_API_KEY"))
	kiteAPISecret := strings.TrimSpace(os.Getenv("KITE_API_SECRET"))
	kiteRedirectURL := strings.TrimSpace(os.Getenv("KITE_REDIRECT_URL"))
	if kiteRedirectURL == "" && frontendURL != "" {
		kiteRedirectURL = frontendURL + "/dashboard"
	}

	return &Config{
		Port:               portStr,
		JWTSecret:          []byte(jwtSecretStr),
		FrontendURL:        frontendURL,
		AllowedOrigins:     allowedOrigins,
		Env:                envStr,
		AccessTokenExpiry:  5 * time.Minute,
		RefreshTokenExpiry: 24 * time.Hour,
		IsProduction:       isProduction,
		AuthUserEmail:      authUserEmail,
		AuthUserPassword:   authUserPassword,
		AuthUserName:       authUserName,
		AuthUserID:         authUserID,
		AuthUserRole:       authUserRole,
		DatabasePath:       dbPath,
		KiteAPIKey:         kiteAPIKey,
		KiteAPISecret:      kiteAPISecret,
		KiteRedirectURL:    kiteRedirectURL,
	}, nil
}
