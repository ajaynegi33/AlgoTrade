package kite

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/ajaynegi33/AlgoTrade/internal/response"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// GetStatus returns the current Kite connection state
func (h *Handler) GetStatus(w http.ResponseWriter, r *http.Request) {
	status := h.service.GetStatus()
	response.Success(w, http.StatusOK, status)
}

// GetProfile returns the real-time Zerodha user profile & funds ledger
func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	profile, err := h.service.GetProfile()
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "KITE_PROFILE_ERROR", err.Error())
		return
	}
	response.Success(w, http.StatusOK, profile)
}

// GetLoginURL returns the Kite Connect OAuth authorization URL
func (h *Handler) GetLoginURL(w http.ResponseWriter, r *http.Request) {
	loginUrl, err := h.service.GetLoginURL()
	if err != nil {
		response.Error(w, http.StatusBadRequest, "KITE_NOT_CONFIGURED", err.Error())
		return
	}

	response.Success(w, http.StatusOK, map[string]string{
		"login_url": loginUrl,
	})
}

// HandleBrowserCallback handles direct browser OAuth redirects from Zerodha Kite (GET)
// and redirects the user back to the frontend dashboard via HTTP 302
func (h *Handler) HandleBrowserCallback(w http.ResponseWriter, r *http.Request) {
	dashboardURL := h.service.cfg.FrontendURL + "/dashboard"

	statusParam := r.URL.Query().Get("status")
	requestToken := r.URL.Query().Get("request_token")

	// 1. Verify status parameter from Zerodha
	if statusParam != "success" || requestToken == "" {
		errMsg := r.URL.Query().Get("message")
		if errMsg == "" {
			errMsg = "Zerodha authentication was declined or cancelled"
		}
		slog.Warn("Zerodha browser callback received non-success status", "status", statusParam, "error", errMsg)
		target := fmt.Sprintf("%s?kite_status=error&message=%s", dashboardURL, url.QueryEscape(errMsg))
		http.Redirect(w, r, target, http.StatusFound)
		return
	}

	// 2. Exchange request_token for active access_token
	session, err := h.service.GenerateSession(requestToken)
	if err != nil {
		slog.Error("Zerodha browser callback session exchange failed", "error", err)
		target := fmt.Sprintf("%s?kite_status=error&message=%s", dashboardURL, url.QueryEscape(err.Error()))
		http.Redirect(w, r, target, http.StatusFound)
		return
	}

	slog.Info("Zerodha browser callback completed successfully, redirecting to dashboard",
		"user_id", session.UserID,
		"user_name", session.UserName,
	)

	// 3. Clean HTTP 302 redirect back to Next.js dashboard
	target := fmt.Sprintf("%s?kite_status=success", dashboardURL)
	http.Redirect(w, r, target, http.StatusFound)
}

type CallbackRequest struct {
	RequestToken string `json:"request_token"`
}

// HandleCallback exchanges the request_token for an access_token via programmatic JSON POST
func (h *Handler) HandleCallback(w http.ResponseWriter, r *http.Request) {
	var req CallbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "INVALID_REQUEST_BODY", "failed to parse json body")
		return
	}

	if req.RequestToken == "" {
		response.Error(w, http.StatusBadRequest, "MISSING_REQUEST_TOKEN", "request_token is required")
		return
	}

	session, err := h.service.GenerateSession(req.RequestToken)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "KITE_AUTH_FAILED", err.Error())
		return
	}

	response.Success(w, http.StatusOK, map[string]any{
		"message":    "Kite authentication successful",
		"user_id":    session.UserID,
		"user_name":  session.UserName,
		"login_time": session.LoginTime,
	})
}
