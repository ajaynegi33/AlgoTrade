package scanner

import (
	"net/http"

	"github.com/ajaynegi33/AlgoTrade/internal/response"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// GetUniverse returns the current F&O cash equity universe for today (or latest cached)
func (h *Handler) GetUniverse(w http.ResponseWriter, r *http.Request) {
	dateParam := r.URL.Query().Get("date")
	universe, err := h.service.GetUniverse(dateParam)
	if err != nil {
		// If not yet scanned for today, attempt an automatic initial scan
		universe, err = h.service.RunScan(false)
		if err != nil {
			response.Error(w, http.StatusNotFound, "UNIVERSE_NOT_FOUND", err.Error())
			return
		}
	}

	response.Success(w, http.StatusOK, universe)
}

// RunScan triggers an on-demand scan of the stock universe
func (h *Handler) RunScan(w http.ResponseWriter, r *http.Request) {
	universe, err := h.service.RunScan(true)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "SCANNER_ERROR", err.Error())
		return
	}

	response.Success(w, http.StatusOK, map[string]any{
		"message":      "08:30 AM Universe scan completed successfully",
		"scan_date":    universe.ScanDate,
		"total_stocks": universe.TotalStocks,
		"last_updated": universe.LastUpdated,
		"universe":     universe.Stocks,
	})
}
