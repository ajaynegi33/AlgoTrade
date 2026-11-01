package kite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/config"
	"github.com/ajaynegi33/AlgoTrade/internal/kiteratelimiter"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

var (
	// ErrKiteNotConfigured is returned when API key or secret is missing
	ErrKiteNotConfigured = errors.New("kite credentials are not configured in environment")
	// ErrSessionExpired is returned when a stored session is older than 06:00 AM IST today
	ErrSessionExpired = errors.New("kite session has expired (sessions reset daily at 06:00 AM IST)")
	// ErrNoSession is returned when no session exists in the database
	ErrNoSession = errors.New("no active kite session found")
)

// SessionInfo holds the persistent session metadata
type SessionInfo struct {
	UserID      string    `json:"user_id"`
	UserName    string    `json:"user_name"`
	APIKey      string    `json:"api_key"`
	AccessToken string    `json:"access_token"`
	PublicToken string    `json:"public_token"`
	LoginTime   string    `json:"login_time"`
	CreatedAt   time.Time `json:"created_at"`
}

// StatusResponse represents the public status of the Kite connection
type StatusResponse struct {
	IsConfigured bool   `json:"is_configured"`
	IsConnected  bool   `json:"is_connected"`
	UserID       string `json:"user_id,omitempty"`
	UserName     string `json:"user_name,omitempty"`
	LoginTime    string `json:"login_time,omitempty"`
}

// EquityMargins holds real-time account balances from Zerodha
type EquityMargins struct {
	Net            float64 `json:"net"`
	AvailableCash  float64 `json:"available_cash"`
	Collateral     float64 `json:"collateral"`
	AdHocMargin    float64 `json:"adhoc_margin"`
	IntradayPayin  float64 `json:"intraday_payin"`
	OpeningBalance float64 `json:"opening_balance"`
	LiveBalance    float64 `json:"live_balance"`
	UtilisedDebits float64 `json:"utilised_debits"`
	UtilisedPayout float64 `json:"utilised_payout"`
	M2MRealised    float64 `json:"m2m_realised"`
	M2MUnrealised  float64 `json:"m2m_unrealised"`
}

// UserProfileData represents the detailed authenticated Zerodha profile & funds
type UserProfileData struct {
	IsConnected   bool           `json:"is_connected"`
	UserID        string         `json:"user_id,omitempty"`
	UserName      string         `json:"user_name,omitempty"`
	UserShortName string         `json:"user_shortname,omitempty"`
	Email         string         `json:"email,omitempty"`
	Broker        string         `json:"broker,omitempty"`
	UserType      string         `json:"user_type,omitempty"`
	AvatarURL     string         `json:"avatar_url,omitempty"`
	Exchanges     []string       `json:"exchanges,omitempty"`
	Products      []string       `json:"products,omitempty"`
	OrderTypes    []string       `json:"order_types,omitempty"`
	LoginTime     string         `json:"login_time,omitempty"`
	EquityMargin  *EquityMargins `json:"equity_margin,omitempty"`
}

type Service struct {
	cfg             *config.Config
	db              *sql.DB
	client          *kiteconnect.Client
	httpClient      *http.Client
	rateLimiter     *kiteratelimiter.CentralKiteDispatcher
	session         *SessionInfo
	onAuthenticated []func()
	mu              sync.RWMutex
}

// NewService instantiates the Kite service and attempts to restore existing sessions from SQLite
func NewService(cfg *config.Config, db *sql.DB) *Service {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   3 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   3 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   7 * time.Second,
	}

	rateLimiter := kiteratelimiter.NewCentralKiteDispatcher()

	service := &Service{
		cfg:         cfg,
		db:          db,
		httpClient:  httpClient,
		rateLimiter: rateLimiter,
	}

	if cfg.IsKiteConfigured() {
		service.client = kiteconnect.New(cfg.KiteAPIKey)
		service.client.SetHTTPClient(httpClient)
		// Attempt session restoration in background or synchronously
		if err := service.restoreSession(); err != nil {
			slog.Info("Kite session not restored", "reason", err.Error())
		}
	} else {
		slog.Warn("Zerodha Kite API credentials not set. Kite features will remain disabled until configured.")
	}

	return service
}

// GetRateLimiter returns the centralized Kite channel rate limiter
func (service *Service) GetRateLimiter() *kiteratelimiter.CentralKiteDispatcher {
	return service.rateLimiter
}

// Stop terminates the centralized rate limiter and background workers
func (service *Service) Stop() {
	if service.rateLimiter != nil {
		service.rateLimiter.Stop()
	}
}

// RegisterOnAuthenticated registers a callback invoked whenever a Kite session becomes active
func (service *Service) RegisterOnAuthenticated(callbackFunction func()) {
	if callbackFunction == nil {
		return
	}
	service.mu.Lock()
	service.onAuthenticated = append(service.onAuthenticated, callbackFunction)
	hasActive := service.session != nil && service.session.AccessToken != "" && !isSessionExpired(service.session.CreatedAt)
	service.mu.Unlock()

	if hasActive {
		go callbackFunction()
	}
}

func (service *Service) notifyAuthenticatedLocked() {
	for _, callbackFunction := range service.onAuthenticated {
		if callbackFunction != nil {
			go callbackFunction()
		}
	}
}

// GetLoginURL returns the official Kite Connect OAuth login URL
func (service *Service) GetLoginURL() (string, error) {
	if !service.cfg.IsKiteConfigured() {
		return "", ErrKiteNotConfigured
	}
	return service.client.GetLoginURL(), nil
}

// GenerateSession performs the OAuth token handshake with Zerodha Kite and persists the session
func (service *Service) GenerateSession(requestToken string) (*SessionInfo, error) {
	service.mu.Lock()
	defer service.mu.Unlock()

	if !service.cfg.IsKiteConfigured() {
		return nil, ErrKiteNotConfigured
	}

	if requestToken == "" {
		return nil, errors.New("request_token cannot be empty")
	}

	// Handshake with Kite Connect API
	userSession, err := service.client.GenerateSession(requestToken, service.cfg.KiteAPISecret)
	if err != nil {
		return nil, fmt.Errorf("kite token exchange failed: %w", err)
	}

	// Login time formatted in ISO8601/RFC3339
	loginTimeStr := userSession.LoginTime.Format(time.RFC3339)
	if loginTimeStr == "" {
		loginTimeStr = time.Now().Format(time.RFC3339)
	}

	session := &SessionInfo{
		UserID:      userSession.UserID,
		UserName:    userSession.UserName,
		APIKey:      service.cfg.KiteAPIKey,
		AccessToken: userSession.AccessToken,
		PublicToken: userSession.PublicToken,
		LoginTime:   loginTimeStr,
		CreatedAt:   time.Now().UTC(),
	}

	// Persist session into SQLite kite_sessions table
	query := `
		INSERT INTO kite_sessions (id, user_id, user_name, api_key, access_token, public_token, login_time, created_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			user_id = excluded.user_id,
			user_name = excluded.user_name,
			api_key = excluded.api_key,
			access_token = excluded.access_token,
			public_token = excluded.public_token,
			login_time = excluded.login_time,
			created_at = CURRENT_TIMESTAMP;
	`
	_, err = service.db.Exec(query, session.UserID, session.UserName, session.APIKey, session.AccessToken, session.PublicToken, session.LoginTime)
	if err != nil {
		slog.Error("Failed to persist kite session to SQLite", "error", err)
		// We still set session in memory so current process works
	}

	service.session = session
	service.client.SetAccessToken(session.AccessToken)
	service.notifyAuthenticatedLocked()

	slog.Info("Zerodha Kite session authenticated and persisted successfully",
		"user_id", session.UserID,
		"user_name", session.UserName,
	)

	return session, nil
}

// GetStatus returns the current connection state and authenticated user details
func (service *Service) GetStatus() StatusResponse {
	service.mu.RLock()
	defer service.mu.RUnlock()

	if !service.cfg.IsKiteConfigured() {
		return StatusResponse{
			IsConfigured: false,
			IsConnected:  false,
		}
	}

	if service.session == nil || service.session.AccessToken == "" {
		return StatusResponse{
			IsConfigured: true,
			IsConnected:  false,
		}
	}

	// Validate IST session expiry (06:00 AM IST boundary)
	if isSessionExpired(service.session.CreatedAt) {
		return StatusResponse{
			IsConfigured: true,
			IsConnected:  false,
		}
	}

	return StatusResponse{
		IsConfigured: true,
		IsConnected:  true,
		UserID:       service.session.UserID,
		UserName:     service.session.UserName,
		LoginTime:    service.session.LoginTime,
	}
}

// GetSession returns a copy of the active session info, or nil if no active session
func (service *Service) GetSession() *SessionInfo {
	service.mu.RLock()
	defer service.mu.RUnlock()
	if service.session == nil || isSessionExpired(service.session.CreatedAt) {
		return nil
	}
	sessionCopy := *service.session
	return &sessionCopy
}

// GetClient returns the underlying initialized KiteConnect client
func (service *Service) GetClient() *kiteconnect.Client {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return service.client
}

// GetProfile fetches the real-time Zerodha user profile and account margins
func (service *Service) GetProfile() (*UserProfileData, error) {
	service.mu.RLock()
	defer service.mu.RUnlock()

	if !service.cfg.IsKiteConfigured() || service.session == nil || service.session.AccessToken == "" || isSessionExpired(service.session.CreatedAt) {
		return &UserProfileData{
			IsConnected: false,
		}, nil
	}

	var profile kiteconnect.UserProfile
	if service.rateLimiter != nil {
		profileResult, dispatchErr := service.rateLimiter.ExecuteAccountSync(context.Background(), func() (interface{}, error) {
			return service.client.GetUserProfile()
		})
		if dispatchErr != nil {
			return nil, fmt.Errorf("failed to fetch user profile from kite: %w", dispatchErr)
		}
		profile = profileResult.(kiteconnect.UserProfile)
	} else {
		var err error
		profile, err = service.client.GetUserProfile()
		if err != nil {
			return nil, fmt.Errorf("failed to fetch user profile from kite: %w", err)
		}
	}

	res := &UserProfileData{
		IsConnected:   true,
		UserID:        profile.UserID,
		UserName:      profile.UserName,
		UserShortName: profile.UserShortName,
		Email:         profile.Email,
		Broker:        profile.Broker,
		UserType:      profile.UserType,
		AvatarURL:     profile.AvatarURL,
		Exchanges:     profile.Exchanges,
		Products:      profile.Products,
		OrderTypes:    profile.OrderTypes,
		LoginTime:     service.session.LoginTime,
	}

	// Fetch real-time margins
	var margins kiteconnect.AllMargins
	var marginsErr error
	if service.rateLimiter != nil {
		marginsResult, dispatchErr := service.rateLimiter.ExecuteAccountSync(context.Background(), func() (interface{}, error) {
			return service.client.GetUserMargins()
		})
		if dispatchErr != nil {
			marginsErr = dispatchErr
		} else {
			margins = marginsResult.(kiteconnect.AllMargins)
		}
	} else {
		margins, marginsErr = service.client.GetUserMargins()
	}
	if marginsErr == nil {
		res.EquityMargin = &EquityMargins{
			Net:            margins.Equity.Net,
			AvailableCash:  margins.Equity.Available.Cash,
			Collateral:     margins.Equity.Available.Collateral,
			AdHocMargin:    margins.Equity.Available.AdHocMargin,
			IntradayPayin:  margins.Equity.Available.IntradayPayin,
			OpeningBalance: margins.Equity.Available.OpeningBalance,
			LiveBalance:    margins.Equity.Available.LiveBalance,
			UtilisedDebits: margins.Equity.Used.Debits,
			UtilisedPayout: margins.Equity.Used.Payout,
			M2MRealised:    margins.Equity.Used.M2MRealised,
			M2MUnrealised:  margins.Equity.Used.M2MUnrealised,
		}
	} else {
		slog.Warn("Failed to fetch equity margins from kite", "error", marginsErr)
	}

	return res, nil
}

// GetEquityMargins fetches live Zerodha equity margins directly
func (service *Service) GetEquityMargins() (*EquityMargins, error) {
	service.mu.RLock()
	client := service.client
	session := service.session
	configured := service.cfg.IsKiteConfigured()
	dispatcher := service.rateLimiter
	service.mu.RUnlock()

	if !configured || client == nil || session == nil || session.AccessToken == "" || isSessionExpired(session.CreatedAt) {
		return nil, ErrNoSession
	}

	var rawMargins kiteconnect.AllMargins
	if dispatcher != nil {
		result, dispatchError := dispatcher.ExecuteAccountSync(context.Background(), func() (interface{}, error) {
			return client.GetUserMargins()
		})
		if dispatchError != nil {
			return nil, fmt.Errorf("failed to fetch equity margins: %w", dispatchError)
		}
		rawMargins = result.(kiteconnect.AllMargins)
	} else {
		var err error
		rawMargins, err = client.GetUserMargins()
		if err != nil {
			return nil, fmt.Errorf("failed to fetch equity margins: %w", err)
		}
	}

	return &EquityMargins{
		Net:            rawMargins.Equity.Net,
		AvailableCash:  rawMargins.Equity.Available.Cash,
		Collateral:     rawMargins.Equity.Available.Collateral,
		AdHocMargin:    rawMargins.Equity.Available.AdHocMargin,
		IntradayPayin:  rawMargins.Equity.Available.IntradayPayin,
		OpeningBalance: rawMargins.Equity.Available.OpeningBalance,
		LiveBalance:    rawMargins.Equity.Available.LiveBalance,
		UtilisedDebits: rawMargins.Equity.Used.Debits,
		UtilisedPayout: rawMargins.Equity.Used.Payout,
		M2MRealised:    rawMargins.Equity.Used.M2MRealised,
		M2MUnrealised:  rawMargins.Equity.Used.M2MUnrealised,
	}, nil
}

// RawModifyOrderPUT executes a direct PUT /orders/{variety}/{order_id} bypassing gokiteconnect's
// omitempty struct tags so price=0.00 and trigger_price=0.00 are explicitly sent when mutating SL to MARKET,
// and attaches market_protection=-1 for MARKET orders as required by Zerodha OMS.
func (service *Service) RawModifyOrderPUT(
	ctx context.Context,
	variety, orderID, orderType string,
	qty int,
	price, triggerPrice float64,
) (string, error) {
	session := service.GetSession()
	if session == nil || session.AccessToken == "" {
		return "", ErrNoSession
	}
	if variety == "" {
		variety = "regular"
	}
	if orderID == "" {
		return "", errors.New("order_id cannot be empty")
	}
	if qty <= 0 {
		return "", fmt.Errorf("invalid order modification quantity: %d", qty)
	}

	form := url.Values{}
	form.Set("order_type", orderType)
	form.Set("quantity", strconv.Itoa(qty))
	form.Set("price", fmt.Sprintf("%.2f", price))
	form.Set("trigger_price", fmt.Sprintf("%.2f", triggerPrice))
	form.Set("validity", "DAY")
	if orderType == "MARKET" {
		form.Set("market_protection", "-1")
	}

	endpoint := fmt.Sprintf("https://api.kite.trade/orders/%s/%s", url.PathEscape(variety), url.PathEscape(orderID))
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to build modify request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("X-Kite-Version", "3")
	request.Header.Set("Authorization", fmt.Sprintf("token %s:%s", session.APIKey, session.AccessToken))

	var responseObj *http.Response
	if service.rateLimiter != nil {
		result, dispatchError := service.rateLimiter.ExecuteEmergencyOrder(ctx, func() (interface{}, error) {
			resp, doErr := service.httpClient.Do(request)
			if doErr != nil {
				return nil, doErr
			}
			if resp.StatusCode == http.StatusTooManyRequests {
				defer resp.Body.Close()
				bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
				return nil, fmt.Errorf("kite HTTP 429 too many requests: %s", string(bodyBytes))
			}
			return resp, nil
		})
		if dispatchError != nil {
			return "", fmt.Errorf("kite modify PUT rate limiter error: %w", dispatchError)
		}
		responseObj = result.(*http.Response)
	} else {
		var err error
		responseObj, err = service.httpClient.Do(request)
		if err != nil {
			return "", fmt.Errorf("kite modify PUT network error: %w", err)
		}
	}
	defer responseObj.Body.Close()

	body, err := io.ReadAll(io.LimitReader(responseObj.Body, 65536))
	if err != nil {
		return "", fmt.Errorf("failed to read modify response body: %w", err)
	}

	if responseObj.StatusCode >= 400 {
		return "", fmt.Errorf("kite PUT /orders/%s/%s failed (HTTP %d): %s", variety, orderID, responseObj.StatusCode, string(body))
	}

	var envelope struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Data    struct {
			OrderID string `json:"order_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", fmt.Errorf("failed to decode kite modify response: %w", err)
	}
	if envelope.Status != "success" && envelope.Data.OrderID == "" {
		return "", fmt.Errorf("kite modify rejected: %s", envelope.Message)
	}
	if envelope.Data.OrderID != "" {
		return envelope.Data.OrderID, nil
	}
	return orderID, nil
}

// HasAPISecret returns true when a Kite API secret is configured for postback SHA-256 verification
func (service *Service) HasAPISecret() bool {
	return service != nil && service.cfg != nil && strings.TrimSpace(service.cfg.KiteAPISecret) != ""
}

// VerifyPostbackChecksum validates Zerodha's SHA-256(order_id + order_timestamp + api_secret) webhook signature
func (service *Service) VerifyPostbackChecksum(orderID, orderTimestamp, checksum string) bool {
	if orderID == "" || orderTimestamp == "" || checksum == "" || service.cfg == nil || service.cfg.KiteAPISecret == "" {
		return false
	}
	raw := orderID + orderTimestamp + service.cfg.KiteAPISecret
	sum := sha256.Sum256([]byte(raw))
	expected := hex.EncodeToString(sum[:])
	return strings.EqualFold(expected, strings.TrimSpace(checksum))
}

// restoreSession reads SQLite for an existing session and checks its IST validity
func (service *Service) restoreSession() error {
	service.mu.Lock()
	defer service.mu.Unlock()

	var session SessionInfo
	query := `SELECT user_id, user_name, api_key, access_token, public_token, login_time, created_at FROM kite_sessions WHERE id = 1`
	row := service.db.QueryRow(query)

	var createdAtStr string
	err := row.Scan(&session.UserID, &session.UserName, &session.APIKey, &session.AccessToken, &session.PublicToken, &session.LoginTime, &createdAtStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoSession
		}
		return fmt.Errorf("failed to query kite session: %w", err)
	}

	// Parse createdAt
	createdAt, parseErr := time.Parse(time.RFC3339, createdAtStr)
	if parseErr != nil {
		createdAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
	}
	session.CreatedAt = createdAt

	// Check if expired at 06:00 AM IST
	if isSessionExpired(session.CreatedAt) {
		return ErrSessionExpired
	}

	service.session = &session
	service.client.SetAccessToken(session.AccessToken)
	slog.Info("Active Zerodha Kite session successfully restored from SQLite",
		"user_id", session.UserID,
		"user_name", session.UserName,
	)
	return nil
}

// isSessionExpired checks if the session was created before the latest 06:00 AM IST cutoff
func isSessionExpired(sessionCreatedAt time.Time) bool {
	istLoc := time.FixedZone("IST", 5*3600+1800)
	nowIST := time.Now().In(istLoc)

	// Calculate today's 06:00 AM IST
	today6AM := time.Date(nowIST.Year(), nowIST.Month(), nowIST.Day(), 6, 0, 0, 0, istLoc)

	var cutoff time.Time
	if nowIST.Before(today6AM) {
		// If current time is before 06:00 AM IST today, cutoff is yesterday 06:00 AM IST
		cutoff = today6AM.AddDate(0, 0, -1)
	} else {
		// If current time is after 06:00 AM IST today, cutoff is today 06:00 AM IST
		cutoff = today6AM
	}

	return sessionCreatedAt.In(istLoc).Before(cutoff)
}
