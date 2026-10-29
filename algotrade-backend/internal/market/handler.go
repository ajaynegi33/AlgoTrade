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
func (handler *Handler) GetStatus(writer http.ResponseWriter, request *http.Request) {
	trackedStocksCount, processedTicksCount := handler.actor.GetStats()
	clientCount := int64(0)
	if handler.wsHub != nil {
		clientCount = handler.wsHub.ClientCount()
	}

	response.JSON(writer, http.StatusOK, map[string]interface{}{
		"status":          "running",
		"tracked_stocks":  trackedStocksCount,
		"ticks_processed": processedTicksCount,
		"active_clients":  clientCount,
	})
}

// GetHistoricalStatus returns whether the Kite Historical API subscription is active
func (handler *Handler) GetHistoricalStatus(writer http.ResponseWriter, request *http.Request) {
	isHealthy, errorMessage := handler.actor.histClient.IsHealthy()
	response.JSON(writer, http.StatusOK, map[string]interface{}{
		"is_active": isHealthy,
		"error":     errorMessage,
	})
}

// GetStockDetail handles GET /api/stocks/{symbol}
func (handler *Handler) GetStockDetail(writer http.ResponseWriter, request *http.Request) {
	symbol := request.PathValue("symbol")
	if symbol == "" {
		response.Error(writer, http.StatusBadRequest, "INVALID_SYMBOL", "Stock symbol is required")
		return
	}

	stockDetail, isStockFound := handler.actor.GetStockDetail(symbol)
	if !isStockFound {
		response.Error(writer, http.StatusNotFound, "STOCK_NOT_FOUND", "Stock not found in tracked F&O universe")
		return
	}

	response.Success(writer, http.StatusOK, stockDetail)
}
