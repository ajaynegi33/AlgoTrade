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
func (h *Handler) GetState(w http.ResponseWriter, r *http.Request) {
	if h.manager == nil {
		response.Error(w, http.StatusServiceUnavailable, "EXECUTION_UNAVAILABLE", "Execution manager is not initialized")
		return
	}
	response.Success(w, http.StatusOK, h.manager.GetCurrentState())
}

// GetHistory handles GET /api/execution/history
func (h *Handler) GetHistory(w http.ResponseWriter, r *http.Request) {
	if h.manager == nil {
		response.Error(w, http.StatusServiceUnavailable, "EXECUTION_UNAVAILABLE", "Execution manager is not initialized")
		return
	}
	q := r.URL.Query()
	mode := q.Get("mode")
	fromDate := q.Get("from")
	toDate := q.Get("to")
	limit := 500
	if lStr := q.Get("limit"); lStr != "" {
		if parsed, err := strconv.Atoi(lStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	history := h.manager.GetTradeHistory(mode, fromDate, toDate, limit)
	response.Success(w, http.StatusOK, history)
}

// UpdateSettings handles POST /api/execution/settings
func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	if h.manager == nil {
		response.Error(w, http.StatusServiceUnavailable, "EXECUTION_UNAVAILABLE", "Execution manager is not initialized")
		return
	}

	var req struct {
		Mode             *string  `json:"mode"`
		AutoExecute      *bool    `json:"auto_execute"`
		PaperCapital     *float64 `json:"paper_capital"`
		RiskPerTradePct  *float64 `json:"risk_per_trade_pct"`
		MaxOpenPositions *int     `json:"max_open_positions"`
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 16384))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "INVALID_BODY", "Failed to read request body")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		response.Error(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON payload")
		return
	}

	cur := h.manager.capitalMgr.GetSettings()
	if req.Mode != nil {
		cur.Mode = *req.Mode
	}
	if req.AutoExecute != nil {
		cur.AutoExecute = *req.AutoExecute
	}
	if req.PaperCapital != nil {
		cur.PaperCapital = *req.PaperCapital
	}
	if req.RiskPerTradePct != nil {
		cur.RiskPerTradePct = *req.RiskPerTradePct
	}
	if req.MaxOpenPositions != nil {
		cur.MaxOpenPositions = *req.MaxOpenPositions
	}

	updatedState, updateErr := h.manager.UpdateSettings(cur)
	if updateErr != nil {
		response.Error(w, http.StatusBadRequest, "SETTINGS_UPDATE_REJECTED", updateErr.Error())
		return
	}

	response.Success(w, http.StatusOK, updatedState)
}

// UpdatePaperCapital handles POST /api/execution/paper-capital
func (h *Handler) UpdatePaperCapital(w http.ResponseWriter, r *http.Request) {
	if h.manager == nil {
		response.Error(w, http.StatusServiceUnavailable, "EXECUTION_UNAVAILABLE", "Execution manager is not initialized")
		return
	}

	var req struct {
		PaperCapital float64 `json:"paper_capital"`
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8192))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "INVALID_BODY", "Failed to read request body")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		response.Error(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON payload")
		return
	}

	updatedState, updateErr := h.manager.UpdatePaperCapital(req.PaperCapital)
	if updateErr != nil {
		response.Error(w, http.StatusBadRequest, "INVALID_CAPITAL", updateErr.Error())
		return
	}

	response.Success(w, http.StatusOK, updatedState)
}

// Flatten handles POST /api/execution/flatten
func (h *Handler) Flatten(w http.ResponseWriter, r *http.Request) {
	if h.manager == nil {
		response.Error(w, http.StatusServiceUnavailable, "EXECUTION_UNAVAILABLE", "Execution manager is not initialized")
		return
	}

	var req struct {
		PositionID string `json:"position_id"`
		All        bool   `json:"all"`
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8192))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "INVALID_BODY", "Failed to read request body")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		response.Error(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON payload")
		return
	}

	if !req.All && req.PositionID == "" {
		response.Error(w, http.StatusBadRequest, "MISSING_TARGET", "Provide position_id or set all=true")
		return
	}

	flattenedCount, flatErr := h.manager.FlattenPosition(req.PositionID, req.All)
	if flatErr != nil {
		response.Error(w, http.StatusNotFound, "FLATTEN_ERROR", flatErr.Error())
		return
	}

	response.Success(w, http.StatusOK, map[string]interface{}{
		"flattened_count": flattenedCount,
		"state":           h.manager.GetCurrentState(),
	})
}

// HandleKitePostback handles POST /api/kite/postback (Zerodha Order Webhook)
func (h *Handler) HandleKitePostback(w http.ResponseWriter, r *http.Request) {
	if h.manager == nil {
		response.Error(w, http.StatusServiceUnavailable, "EXECUTION_UNAVAILABLE", "Execution manager is not initialized")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 65536))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "INVALID_BODY", "Failed to read webhook body")
		return
	}

	var ev OrderUpdateEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		response.Error(w, http.StatusBadRequest, "INVALID_JSON", "Invalid webhook payload")
		return
	}

	if err := h.manager.HandleKiteWebhookPostback(ev); err != nil {
		response.Error(w, http.StatusForbidden, "CHECKSUM_FAILED", err.Error())
		return
	}

	response.Success(w, http.StatusOK, map[string]string{"status": "ok"})
}
