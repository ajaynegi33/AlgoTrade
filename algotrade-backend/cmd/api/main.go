package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/auth"
	"github.com/ajaynegi33/AlgoTrade/internal/config"
	"github.com/ajaynegi33/AlgoTrade/internal/database"
	"github.com/ajaynegi33/AlgoTrade/internal/engine"
	"github.com/ajaynegi33/AlgoTrade/internal/kite"
	"github.com/ajaynegi33/AlgoTrade/internal/response"
	"github.com/ajaynegi33/AlgoTrade/internal/scanner"
	"github.com/ajaynegi33/AlgoTrade/internal/user"
	"github.com/ajaynegi33/AlgoTrade/internal/ws"
)

func main() {
	// Initialize structured logging (slog)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// Strictly load and validate environment variables from .env / OS environment
	cfg, err := config.LoadAndValidate()
	if err != nil {
		if valErr, ok := errors.AsType[*config.ConfigValidationError](err); ok {
			slog.Error("CRITICAL: Server configuration validation failed. Server will not start.",
				"missing_fields", valErr.MissingFields,
				"invalid_fields", valErr.InvalidFields,
			)
		} else {
			slog.Error("CRITICAL: Failed to load server configuration. Server will not start.", "error", err)
		}
		os.Exit(1)
	}

	// Initialize Universal SQLite Database (WAL mode, connection pool, auto-migrations)
	db, err := database.Init(cfg.DatabasePath)
	if err != nil {
		slog.Error("CRITICAL: Failed to initialize SQLite database", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := database.Close(); err != nil {
			slog.Error("Error closing database connection", "error", err)
		}
	}()

	// Initialize user store dynamically from validated configuration
	userStore, err := user.NewMemoryStore(cfg)
	if err != nil {
		slog.Error("CRITICAL: Failed to initialize user store from configuration", "error", err)
		os.Exit(1)
	}

	// Initialize Kite Connect Service
	kiteService := kite.NewService(cfg, db)
	kiteHandler := kite.NewHandler(kiteService)

	// Initialize Stock Universe Scanner Service
	scannerService := scanner.NewService(db, kiteService)
	scannerHandler := scanner.NewHandler(scannerService)

	// Initialize Kite Ticker WebSocket Client (Single shared broker connection)
	tickerClient := kite.NewTickerClient(kiteService)
	tickerClient.Start()
	defer tickerClient.Stop()

	// Initialize Kite Historical Rate-Limited Service (2.5 req/s)
	histService := kite.NewHistoricalService(db, kiteService)

	// Initialize Dedicated Client WebSocket Hub
	wsHub := ws.NewClientHub()

	// Initialize Single-Writer Quant Engine Actor
	engineActor := engine.NewActor(db, tickerClient, histService, wsHub, scannerService)
	if err := engineActor.Start(); err != nil {
		slog.Error("Failed to start Engine Actor", "error", err)
	}
	defer engineActor.Stop()

	engineHandler := engine.NewHandler(engineActor, wsHub)

	// Initialize Handlers
	authHandler := auth.NewHandler(cfg, userStore)
	userHandler := user.NewHandler(userStore)

	mux := http.NewServeMux()

	// Public Health Check
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		response.Success(w, http.StatusOK, map[string]string{
			"status":    "healthy",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	})

	// Public Auth Routes (Method routing natively enforced by ServeMux)
	mux.HandleFunc("POST /api/auth/login", authHandler.Login)
	mux.HandleFunc("POST /api/auth/refresh", authHandler.Refresh)
	mux.HandleFunc("POST /api/auth/logout", authHandler.Logout)

	// Protected User Routes
	requireAuth := auth.AuthMiddleware(cfg.JWTSecret)
	mux.Handle("GET /api/user/me", requireAuth(http.HandlerFunc(userHandler.GetProfile)))
	mux.Handle("GET /api/user/profile", requireAuth(http.HandlerFunc(userHandler.GetProfile)))

	// Zerodha Kite Connect Routes
	mux.HandleFunc("GET /api/kite/status", kiteHandler.GetStatus)
	mux.HandleFunc("GET /api/kite/login-url", kiteHandler.GetLoginURL)
	mux.HandleFunc("GET /api/kite/callback", kiteHandler.HandleBrowserCallback) // Direct browser redirect from Zerodha
	mux.HandleFunc("GET /api/kite/profile", kiteHandler.GetProfile)             // Real Zerodha profile & account margins
	mux.Handle("POST /api/kite/callback", requireAuth(http.HandlerFunc(kiteHandler.HandleCallback)))

	// Stock Universe & 08:30 AM Scanner Routes
	mux.HandleFunc("GET /api/scanner/universe", scannerHandler.GetUniverse)
	mux.Handle("POST /api/scanner/run", requireAuth(http.HandlerFunc(scannerHandler.RunScan)))

	// Real-Time Engine & WebSocket Stream Routes
	mux.HandleFunc("GET /ws/live", wsHub.ServeWS)
	mux.HandleFunc("GET /api/engine/status", engineHandler.GetStatus)
	mux.HandleFunc("GET /api/engine/historical-status", engineHandler.GetHistoricalStatus)

	// Deferred non-blocking initial scan check on startup
	go func() {
		time.Sleep(1 * time.Second)
		_, err := scannerService.RunScan(false)
		if err != nil {
			slog.Info("Initial universe scan deferred (no network or already scanned)", "info", err.Error())
		}
	}()

	// Compose global middlewares: Recovery -> CORS -> Router
	var rootHandler http.Handler = mux
	rootHandler = auth.CORSMiddleware(cfg.FrontendURL)(rootHandler)
	rootHandler = auth.RecoveryMiddleware(rootHandler)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           rootHandler,
		ReadHeaderTimeout: 3 * time.Second, // Slowloris mitigation
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Signal-bound context for clean graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("AlgoTrade Backend server started",
			"port", cfg.Port,
			"env", cfg.Env,
			"auth", "AccessToken=5m, RefreshToken=24h (RTR enabled)",
			"allowed_origin", cfg.FrontendURL,
		)

		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Server encountered fatal error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done() // Block until interrupt received
	slog.Info("Shutdown signal received, shutting down gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("Graceful shutdown error", "error", err)
	} else {
		slog.Info("Server stopped cleanly")
	}
}
