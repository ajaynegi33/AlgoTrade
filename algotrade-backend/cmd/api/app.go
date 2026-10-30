package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/config"
	"github.com/ajaynegi33/AlgoTrade/internal/database"
	"github.com/ajaynegi33/AlgoTrade/internal/diagnostics"
	"github.com/ajaynegi33/AlgoTrade/internal/kite"
	"github.com/ajaynegi33/AlgoTrade/internal/market"
	"github.com/ajaynegi33/AlgoTrade/internal/monitoring"
	"github.com/ajaynegi33/AlgoTrade/internal/screener"
	"github.com/ajaynegi33/AlgoTrade/internal/trading"
	"github.com/ajaynegi33/AlgoTrade/internal/user"
	"github.com/ajaynegi33/AlgoTrade/internal/ws"
)

// App encapsulates the application dependencies and runtime components
type App struct {
	cfg                   *config.Config
	db                    *sql.DB
	server                *http.Server
	tickerClient          *kite.TickerClient
	marketHub             *market.Actor
	tradeManager          *trading.Manager
	screenerService       *screener.Service
	collector             *monitoring.EventCollector
	scheduler             *monitoring.EODScheduler
	flightRecorderManager *diagnostics.FlightRecorderManager
}

// NewApp instantiates all services, wires dependencies, and builds the HTTP server
func NewApp(cfg *config.Config) (*App, error) {
	db, err := database.Init(cfg.DatabasePath)
	if err != nil {
		return nil, err
	}

	userStore, err := user.NewMemoryStore(cfg)
	if err != nil {
		_ = database.Close()
		return nil, err
	}

	kiteService := kite.NewService(cfg, db)
	screenerService := screener.NewService(db, kiteService)
	tickerClient := kite.NewTickerClient(kiteService)
	histService := kite.NewHistoricalService(db, kiteService)
	wsHub := ws.NewClientHub()

	marketHub := market.NewActor(db, tickerClient, histService, wsHub, screenerService)
	tradeManager := trading.NewManager(db, marketHub, kiteService, tickerClient)

	// Initialize Monitoring EventCollector & EOD Scheduler
	collector := monitoring.NewEventCollector(db)
	monitoring.SetCollector(collector)
	collector.Start(context.Background())

	scheduler := monitoring.NewEODScheduler(db, "reports")
	scheduler.Start(context.Background())

	// Initialize Runtime Tracer Flight Recorder Manager
	flightRecorderConfig := diagnostics.DefaultFlightRecorderConfig()
	flightRecorderManager := diagnostics.NewFlightRecorderManager(flightRecorderConfig)
	diagnostics.SetGlobalRecorder(flightRecorderManager)

	// Re-seed historical baselines & scanned universe on Zerodha OAuth login
	kiteService.RegisterOnAuthenticated(func() {
		marketHub.OnKiteAuthenticated()
	})

	handler := buildRouter(cfg, db, userStore, kiteService, screenerService, marketHub, tradeManager, wsHub, scheduler, flightRecorderManager)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return &App{
		cfg:                   cfg,
		db:                    db,
		server:                server,
		tickerClient:          tickerClient,
		marketHub:             marketHub,
		tradeManager:          tradeManager,
		screenerService:       screenerService,
		collector:             collector,
		scheduler:             scheduler,
		flightRecorderManager: flightRecorderManager,
	}, nil
}

// Start launches the ticker, market hub, initial scan, flight recorder, and HTTP server
func (app *App) Start() error {
	if app.flightRecorderManager != nil {
		if startError := app.flightRecorderManager.Start(context.Background()); startError != nil {
			slog.Warn("Failed to start flight recorder manager", "error", startError)
		}
	}

	app.tickerClient.Start()

	if err := app.marketHub.Start(); err != nil {
		return err
	}

	// Deferred non-blocking initial scan check on startup
	go func() {
		time.Sleep(1 * time.Second)
		if _, err := app.screenerService.RunScan(false); err != nil {
			slog.Info("Initial universe scan deferred (no network or already scanned)", "info", err.Error())
		}
	}()

	go func() {
		slog.Info("AlgoTrade Backend started",
			"port", app.cfg.Port,
			"env", app.cfg.Env,
			"auth", "AccessToken=5m, RefreshToken=24h (RTR enabled)",
			"allowed_origin", app.cfg.FrontendURL,
		)
		if err := app.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Server encountered fatal error", "error", err)
			os.Exit(1)
		}
	}()

	return nil
}

// Stop shuts down the HTTP server and all background engines cleanly
func (app *App) Stop(ctx context.Context) {
	slog.Info("Shutdown signal received, shutting down gracefully...")

	if err := app.server.Shutdown(ctx); err != nil {
		slog.Error("Graceful shutdown error", "error", err)
	}

	if app.scheduler != nil {
		app.scheduler.Stop()
	}
	if app.collector != nil {
		app.collector.Stop()
	}
	if app.flightRecorderManager != nil {
		_ = app.flightRecorderManager.Stop()
	}

	app.tradeManager.Stop()
	app.marketHub.Stop()
	app.tickerClient.Stop()

	if err := database.Close(); err != nil {
		slog.Error("Error closing database connection", "error", err)
	}

	slog.Info("Server stopped cleanly")
}

