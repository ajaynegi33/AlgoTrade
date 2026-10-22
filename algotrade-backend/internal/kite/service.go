package kite

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/config"
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
	OpeningBalance float64 `json:"opening_balance"`
	LiveBalance    float64 `json:"live_balance"`
	UtilisedDebits float64 `json:"utilised_debits"`
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
	cfg     *config.Config
	db      *sql.DB
	client  *kiteconnect.Client
	session *SessionInfo
	mu      sync.RWMutex
}

// NewService instantiates the Kite service and attempts to restore existing sessions from SQLite
func NewService(cfg *config.Config, db *sql.DB) *Service {
	s := &Service{
		cfg: cfg,
		db:  db,
	}

	if cfg.IsKiteConfigured() {
		s.client = kiteconnect.New(cfg.KiteAPIKey)
		// Attempt session restoration in background or synchronously
		if err := s.restoreSession(); err != nil {
			slog.Info("Kite session not restored", "reason", err.Error())
		}
	} else {
		slog.Warn("Zerodha Kite API credentials not set. Kite features will remain disabled until configured.")
	}

	return s
}

// GetLoginURL returns the official Kite Connect OAuth login URL
func (s *Service) GetLoginURL() (string, error) {
	if !s.cfg.IsKiteConfigured() {
		return "", ErrKiteNotConfigured
	}
	return s.client.GetLoginURL(), nil
}

// GenerateSession performs the OAuth token handshake with Zerodha Kite and persists the session
func (s *Service) GenerateSession(requestToken string) (*SessionInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.cfg.IsKiteConfigured() {
		return nil, ErrKiteNotConfigured
	}

	if requestToken == "" {
		return nil, errors.New("request_token cannot be empty")
	}

	// Handshake with Kite Connect API
	userSession, err := s.client.GenerateSession(requestToken, s.cfg.KiteAPISecret)
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
		APIKey:      s.cfg.KiteAPIKey,
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
	_, err = s.db.Exec(query, session.UserID, session.UserName, session.APIKey, session.AccessToken, session.PublicToken, session.LoginTime)
	if err != nil {
		slog.Error("Failed to persist kite session to SQLite", "error", err)
		// We still set session in memory so current process works
	}

	s.session = session
	s.client.SetAccessToken(session.AccessToken)

	slog.Info("Zerodha Kite session authenticated and persisted successfully",
		"user_id", session.UserID,
		"user_name", session.UserName,
	)

	return session, nil
}

// GetStatus returns the current connection state and authenticated user details
func (s *Service) GetStatus() StatusResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.cfg.IsKiteConfigured() {
		return StatusResponse{
			IsConfigured: false,
			IsConnected:  false,
		}
	}

	if s.session == nil || s.session.AccessToken == "" {
		return StatusResponse{
			IsConfigured: true,
			IsConnected:  false,
		}
	}

	// Validate IST session expiry (06:00 AM IST boundary)
	if isSessionExpired(s.session.CreatedAt) {
		return StatusResponse{
			IsConfigured: true,
			IsConnected:  false,
		}
	}

	return StatusResponse{
		IsConfigured: true,
		IsConnected:  true,
		UserID:       s.session.UserID,
		UserName:     s.session.UserName,
		LoginTime:    s.session.LoginTime,
	}
}

// GetClient returns the underlying initialized KiteConnect client
func (s *Service) GetClient() *kiteconnect.Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.client
}

// GetProfile fetches the real-time Zerodha user profile and account margins
func (s *Service) GetProfile() (*UserProfileData, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.cfg.IsKiteConfigured() || s.session == nil || s.session.AccessToken == "" || isSessionExpired(s.session.CreatedAt) {
		return &UserProfileData{
			IsConnected: false,
		}, nil
	}

	profile, err := s.client.GetUserProfile()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user profile from kite: %w", err)
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
		LoginTime:     s.session.LoginTime,
	}

	// Fetch real-time margins
	margins, err := s.client.GetUserMargins()
	if err == nil {
		res.EquityMargin = &EquityMargins{
			Net:            margins.Equity.Net,
			AvailableCash:  margins.Equity.Available.Cash,
			Collateral:     margins.Equity.Available.Collateral,
			OpeningBalance: margins.Equity.Available.OpeningBalance,
			LiveBalance:    margins.Equity.Available.LiveBalance,
			UtilisedDebits: margins.Equity.Used.Debits,
		}
	} else {
		slog.Warn("Failed to fetch equity margins from kite", "error", err)
	}

	return res, nil
}

// restoreSession reads SQLite for an existing session and checks its IST validity
func (s *Service) restoreSession() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var session SessionInfo
	query := `SELECT user_id, user_name, api_key, access_token, public_token, login_time, created_at FROM kite_sessions WHERE id = 1`
	row := s.db.QueryRow(query)

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

	s.session = &session
	s.client.SetAccessToken(session.AccessToken)
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
