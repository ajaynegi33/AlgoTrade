package main

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/auth"
	"github.com/ajaynegi33/AlgoTrade/internal/config"
	"github.com/ajaynegi33/AlgoTrade/internal/kite"
	"github.com/ajaynegi33/AlgoTrade/internal/market"
	"github.com/ajaynegi33/AlgoTrade/internal/response"
	"github.com/ajaynegi33/AlgoTrade/internal/screener"
	"github.com/ajaynegi33/AlgoTrade/internal/trading"
	"github.com/ajaynegi33/AlgoTrade/internal/user"
	"github.com/ajaynegi33/AlgoTrade/internal/ws"
)

// buildRouter constructs the HTTP ServeMux, binds routes and aliases, and applies middlewares
func buildRouter(
	cfg *config.Config,
	_ *sql.DB,
	userStore user.Store,
	kiteService *kite.Service,
	screenerService *screener.Service,
	marketHub *market.Actor,
	tradeManager *trading.Manager,
	wsHub *ws.ClientHub,
) http.Handler {
	mux := http.NewServeMux()

	// Initialize Handlers
	authHandler := auth.NewHandler(cfg, userStore)
	userHandler := user.NewHandler(userStore)
	kiteHandler := kite.NewHandler(kiteService)
	screenerHandler := screener.NewHandler(screenerService)
	marketHandler := market.NewHandler(marketHub, wsHub)
	tradeHandler := trading.NewHandler(tradeManager)

	// Auth Middleware
	requireAuth := auth.AuthMiddleware(cfg.JWTSecret)

	// Public Health Check
	mux.HandleFunc("GET /api/health", func(writer http.ResponseWriter, request *http.Request) {
		response.Success(writer, http.StatusOK, map[string]string{
			"status":    "healthy",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	})

	// Public Auth Routes
	mux.HandleFunc("POST /api/auth/login", authHandler.Login)
	mux.HandleFunc("POST /api/auth/refresh", authHandler.Refresh)
	mux.HandleFunc("POST /api/auth/logout", authHandler.Logout)

	// Protected User Routes
	mux.Handle("GET /api/user/me", requireAuth(http.HandlerFunc(userHandler.GetProfile)))

	// Zerodha Kite Connect Routes
	mux.HandleFunc("GET /api/kite/status", kiteHandler.GetStatus)
	mux.HandleFunc("GET /api/kite/login-url", kiteHandler.GetLoginURL)
	mux.HandleFunc("GET /api/kite/callback", kiteHandler.HandleBrowserCallback)
	mux.HandleFunc("GET /api/kite/profile", kiteHandler.GetProfile)

	// Screener & Universe Routes (with legacy scanner aliases)
	mux.HandleFunc("GET /api/screener/universe", screenerHandler.GetUniverse)
	mux.HandleFunc("GET /api/scanner/universe", screenerHandler.GetUniverse)
	mux.Handle("POST /api/screener/run", requireAuth(http.HandlerFunc(screenerHandler.RunScan)))
	mux.Handle("POST /api/scanner/run", requireAuth(http.HandlerFunc(screenerHandler.RunScan)))

	// Real-Time Market & WebSocket Stream Routes (with legacy engine aliases)
	mux.HandleFunc("GET /ws/live", wsHub.ServeWS)
	mux.HandleFunc("GET /api/market/status", marketHandler.GetStatus)
	mux.HandleFunc("GET /api/engine/status", marketHandler.GetStatus)
	mux.HandleFunc("GET /api/market/historical-status", marketHandler.GetHistoricalStatus)
	mux.HandleFunc("GET /api/engine/historical-status", marketHandler.GetHistoricalStatus)
	mux.HandleFunc("GET /api/stocks/{symbol}", marketHandler.GetStockDetail)

	// Trading & Risk Management Routes (with legacy execution aliases)
	mux.HandleFunc("GET /api/trading/state", tradeHandler.GetState)
	mux.HandleFunc("GET /api/execution/state", tradeHandler.GetState)
	mux.HandleFunc("GET /api/trading/history", tradeHandler.GetHistory)
	mux.HandleFunc("GET /api/execution/history", tradeHandler.GetHistory)
	mux.Handle("POST /api/trading/settings", requireAuth(http.HandlerFunc(tradeHandler.UpdateSettings)))
	mux.Handle("POST /api/execution/settings", requireAuth(http.HandlerFunc(tradeHandler.UpdateSettings)))
	mux.Handle("POST /api/trading/paper-capital", requireAuth(http.HandlerFunc(tradeHandler.UpdatePaperCapital)))
	mux.Handle("POST /api/execution/paper-capital", requireAuth(http.HandlerFunc(tradeHandler.UpdatePaperCapital)))
	mux.Handle("POST /api/trading/flatten", requireAuth(http.HandlerFunc(tradeHandler.Flatten)))
	mux.Handle("POST /api/execution/flatten", requireAuth(http.HandlerFunc(tradeHandler.Flatten)))
	mux.HandleFunc("POST /api/kite/postback", tradeHandler.HandleKitePostback)

	// Market Structure & Order Block Routes (legacy compatibility - returns empty list)
	mux.HandleFunc("GET /api/structure/orderblock-tree", func(writer http.ResponseWriter, request *http.Request) {
		response.Success(writer, http.StatusOK, []any{})
	})
	mux.HandleFunc("GET /api/prediction/orderblock-tree", func(writer http.ResponseWriter, request *http.Request) {
		response.Success(writer, http.StatusOK, []any{})
	})

	// Wrap Global Middlewares: Recovery -> CORS -> Router
	var rootHandler http.Handler = mux
	rootHandler = auth.CORSMiddleware(cfg.FrontendURL)(rootHandler)
	rootHandler = auth.RecoveryMiddleware(rootHandler)

	return rootHandler
}

