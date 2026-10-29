package market

import (
	"net/http"

	"github.com/ajaynegi33/AlgoTrade/internal/response"
	"github.com/ajaynegi33/AlgoTrade/internal/ws"
)

// Handler exposes HTTP management endpoints for the quant engine
type Handler struct {
	actor *Actor
	wsHub *ws.ClientHub
}

// NewHandler creates a new Engine HTTP handler
func NewHandler(actor *Actor, wsHub *ws.ClientHub) *Handler {
	return &Handler{
		actor: actor,
		wsHub: wsHub,
	}
}

// GetStatus returns real-time health and throughput metrics
func (h *Handler) GetStatus(w http.ResponseWriter, r *http.Request) {
	tracked, ticks := h.actor.GetStats()
	clientCount := int64(0)
	if h.wsHub != nil {
		clientCount = h.wsHub.ClientCount()
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"status":          "running",
		"tracked_stocks":  tracked,
		"ticks_processed": ticks,
		"active_clients":  clientCount,
	})
}

// GetHistoricalStatus returns whether the Kite Historical API subscription is active
func (h *Handler) GetHistoricalStatus(w http.ResponseWriter, r *http.Request) {
	isHealthy, errMsg := h.actor.histClient.IsHealthy()
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"is_active": isHealthy,
		"error":     errMsg,
	})
}

// GetStockDetail handles GET /api/stocks/{symbol}
func (h *Handler) GetStockDetail(w http.ResponseWriter, r *http.Request) {
	symbol := r.PathValue("symbol")
	if symbol == "" {
		response.Error(w, http.StatusBadRequest, "INVALID_SYMBOL", "Stock symbol is required")
		return
	}

	detail, found := h.actor.GetStockDetail(symbol)
	if !found {
		response.Error(w, http.StatusNotFound, "STOCK_NOT_FOUND", "Stock not found in tracked F&O universe")
		return
	}

	response.Success(w, http.StatusOK, detail)
}
