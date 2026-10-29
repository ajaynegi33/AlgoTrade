package kite

import (
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
func (handler *Handler) GetStatus(writer http.ResponseWriter, request *http.Request) {
	status := handler.service.GetStatus()
	response.Success(writer, http.StatusOK, status)
}

// GetProfile returns the real-time Zerodha user profile & funds ledger
func (handler *Handler) GetProfile(writer http.ResponseWriter, request *http.Request) {
	profile, err := handler.service.GetProfile()
	if err != nil {
		response.Error(writer, http.StatusInternalServerError, "KITE_PROFILE_ERROR", err.Error())
		return
	}
	response.Success(writer, http.StatusOK, profile)
}

// GetLoginURL returns the Kite Connect OAuth authorization URL
func (handler *Handler) GetLoginURL(writer http.ResponseWriter, request *http.Request) {
	loginURL, err := handler.service.GetLoginURL()
	if err != nil {
		response.Error(writer, http.StatusBadRequest, "KITE_NOT_CONFIGURED", err.Error())
		return
	}

	response.Success(writer, http.StatusOK, map[string]string{
		"login_url": loginURL,
	})
}

// HandleBrowserCallback handles direct browser OAuth redirects from Zerodha Kite (GET)
// and redirects the user back to the frontend dashboard via HTTP 302
func (handler *Handler) HandleBrowserCallback(writer http.ResponseWriter, request *http.Request) {
	dashboardURL := handler.service.cfg.FrontendURL + "/dashboard"

	statusParam := request.URL.Query().Get("status")
	requestToken := request.URL.Query().Get("request_token")

	// 1. Verify status parameter from Zerodha
	if statusParam != "success" || requestToken == "" {
		errorMessage := request.URL.Query().Get("message")
		if errorMessage == "" {
			errorMessage = "Zerodha authentication was declined or cancelled"
		}
		slog.Warn("Zerodha browser callback received non-success status", "status", statusParam, "error", errorMessage)
		redirectTargetURL := fmt.Sprintf("%s?kite_status=error&message=%s", dashboardURL, url.QueryEscape(errorMessage))
		http.Redirect(writer, request, redirectTargetURL, http.StatusFound)
		return
	}

	// 2. Exchange request_token for active access_token
	session, err := handler.service.GenerateSession(requestToken)
	if err != nil {
		slog.Error("Zerodha browser callback session exchange failed", "error", err)
		redirectTargetURL := fmt.Sprintf("%s?kite_status=error&message=%s", dashboardURL, url.QueryEscape(err.Error()))
		http.Redirect(writer, request, redirectTargetURL, http.StatusFound)
		return
	}

	slog.Info("Zerodha browser callback completed successfully, redirecting to dashboard",
		"user_id", session.UserID,
		"user_name", session.UserName,
	)

	// 3. Clean HTTP 302 redirect back to Next.js dashboard
	redirectTargetURL := fmt.Sprintf("%s?kite_status=success", dashboardURL)
	http.Redirect(writer, request, redirectTargetURL, http.StatusFound)
}

