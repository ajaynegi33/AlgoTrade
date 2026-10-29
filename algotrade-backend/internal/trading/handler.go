package trading

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/ajaynegi33/AlgoTrade/internal/response"
)

// Handler exposes HTTP REST endpoints for Phase 2 Autonomous Execution & Kite Webhook Postbacks
type Handler struct {
	manager *Manager
}

func NewHandler(manager *Manager) *Handler {
	return &Handler{
		manager: manager,
	}
}

// GetState handles GET /api/execution/state
func (handler *Handler) GetState(writer http.ResponseWriter, request *http.Request) {
	if handler.manager == nil {
		response.Error(writer, http.StatusServiceUnavailable, "EXECUTION_UNAVAILABLE", "Execution manager is not initialized")
		return
	}
	response.Success(writer, http.StatusOK, handler.manager.GetCurrentState())
}

// GetHistory handles GET /api/execution/history
func (handler *Handler) GetHistory(writer http.ResponseWriter, request *http.Request) {
	if handler.manager == nil {
		response.Error(writer, http.StatusServiceUnavailable, "EXECUTION_UNAVAILABLE", "Execution manager is not initialized")
		return
	}
	queryParams := request.URL.Query()
	mode := queryParams.Get("mode")
	fromDate := queryParams.Get("from")
	toDate := queryParams.Get("to")
	limit := 500
	if limitString := queryParams.Get("limit"); limitString != "" {
		if parsed, err := strconv.Atoi(limitString); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	history := handler.manager.GetTradeHistory(mode, fromDate, toDate, limit)
	response.Success(writer, http.StatusOK, history)
}

// UpdateSettings handles POST /api/execution/settings
func (handler *Handler) UpdateSettings(writer http.ResponseWriter, request *http.Request) {
	if handler.manager == nil {
		response.Error(writer, http.StatusServiceUnavailable, "EXECUTION_UNAVAILABLE", "Execution manager is not initialized")
		return
	}

	var requestBody struct {
		Mode             *string  `json:"mode"`
		AutoExecute      *bool    `json:"auto_execute"`
		PaperCapital     *float64 `json:"paper_capital"`
		RiskPerTradePct  *float64 `json:"risk_per_trade_pct"`
		MaxOpenPositions *int     `json:"max_open_positions"`
	}
	bodyBytes, err := io.ReadAll(io.LimitReader(request.Body, 16384))
	if err != nil {
		response.Error(writer, http.StatusBadRequest, "INVALID_BODY", "Failed to read request body")
		return
	}
	if err := json.Unmarshal(bodyBytes, &requestBody); err != nil {
		response.Error(writer, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON payload")
		return
	}

	currentSettings := handler.manager.capitalMgr.GetSettings()
	if requestBody.Mode != nil {
		currentSettings.Mode = *requestBody.Mode
	}
	if requestBody.AutoExecute != nil {
		currentSettings.AutoExecute = *requestBody.AutoExecute
	}
	if requestBody.PaperCapital != nil {
		currentSettings.PaperCapital = *requestBody.PaperCapital
	}
	if requestBody.RiskPerTradePct != nil {
		currentSettings.RiskPerTradePct = *requestBody.RiskPerTradePct
	}
	if requestBody.MaxOpenPositions != nil {
		currentSettings.MaxOpenPositions = *requestBody.MaxOpenPositions
	}

	updatedState, updateErr := handler.manager.UpdateSettings(currentSettings)
	if updateErr != nil {
		response.Error(writer, http.StatusBadRequest, "SETTINGS_UPDATE_REJECTED", updateErr.Error())
		return
	}

	response.Success(writer, http.StatusOK, updatedState)
}

// UpdatePaperCapital handles POST /api/execution/paper-capital
func (handler *Handler) UpdatePaperCapital(writer http.ResponseWriter, request *http.Request) {
	if handler.manager == nil {
		response.Error(writer, http.StatusServiceUnavailable, "EXECUTION_UNAVAILABLE", "Execution manager is not initialized")
		return
	}

	var requestBody struct {
		PaperCapital float64 `json:"paper_capital"`
	}
	bodyBytes, err := io.ReadAll(io.LimitReader(request.Body, 8192))
	if err != nil {
		response.Error(writer, http.StatusBadRequest, "INVALID_BODY", "Failed to read request body")
		return
	}
	if err := json.Unmarshal(bodyBytes, &requestBody); err != nil {
		response.Error(writer, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON payload")
		return
	}

	updatedState, updateErr := handler.manager.UpdatePaperCapital(requestBody.PaperCapital)
	if updateErr != nil {
		response.Error(writer, http.StatusBadRequest, "INVALID_CAPITAL", updateErr.Error())
		return
	}

	response.Success(writer, http.StatusOK, updatedState)
}

// Flatten handles POST /api/execution/flatten
func (handler *Handler) Flatten(writer http.ResponseWriter, request *http.Request) {
	if handler.manager == nil {
		response.Error(writer, http.StatusServiceUnavailable, "EXECUTION_UNAVAILABLE", "Execution manager is not initialized")
		return
	}

	var requestBody struct {
		PositionID string `json:"position_id"`
		All        bool   `json:"all"`
	}
	bodyBytes, err := io.ReadAll(io.LimitReader(request.Body, 8192))
	if err != nil {
		response.Error(writer, http.StatusBadRequest, "INVALID_BODY", "Failed to read request body")
		return
	}
	if err := json.Unmarshal(bodyBytes, &requestBody); err != nil {
		response.Error(writer, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON payload")
		return
	}

	if !requestBody.All && requestBody.PositionID == "" {
		response.Error(writer, http.StatusBadRequest, "MISSING_TARGET", "Provide position_id or set all=true")
		return
	}

	flattenedCount, flatErr := handler.manager.FlattenPosition(requestBody.PositionID, requestBody.All)
	if flatErr != nil {
		response.Error(writer, http.StatusNotFound, "FLATTEN_ERROR", flatErr.Error())
		return
	}

	response.Success(writer, http.StatusOK, map[string]interface{}{
		"flattened_count": flattenedCount,
		"state":           handler.manager.GetCurrentState(),
	})
}

// HandleKitePostback handles POST /api/kite/postback (Zerodha Order Webhook)
func (handler *Handler) HandleKitePostback(writer http.ResponseWriter, request *http.Request) {
	if handler.manager == nil {
		response.Error(writer, http.StatusServiceUnavailable, "EXECUTION_UNAVAILABLE", "Execution manager is not initialized")
		return
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(request.Body, 65536))
	if err != nil {
		response.Error(writer, http.StatusBadRequest, "INVALID_BODY", "Failed to read webhook body")
		return
	}

	var orderEvent OrderUpdateEvent
	if err := json.Unmarshal(bodyBytes, &orderEvent); err != nil {
		response.Error(writer, http.StatusBadRequest, "INVALID_JSON", "Invalid webhook payload")
		return
	}

	if err := handler.manager.HandleKiteWebhookPostback(orderEvent); err != nil {
		response.Error(writer, http.StatusForbidden, "CHECKSUM_FAILED", err.Error())
		return
	}

	response.Success(writer, http.StatusOK, map[string]string{"status": "ok"})
}
