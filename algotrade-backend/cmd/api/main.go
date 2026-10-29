package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/config"
)

func main() {
	// 1. Structured logging
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// 2. Validate environment configuration
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

	// 3. Initialize application dependency container
	app, err := NewApp(cfg)
	if err != nil {
		slog.Error("CRITICAL: Failed to initialize application", "error", err)
		os.Exit(1)
	}

	// 4. Start background engines and HTTP listener
	if err := app.Start(); err != nil {
		slog.Error("CRITICAL: Failed to start application", "error", err)
		os.Exit(1)
	}

	// 5. Block on OS interrupt signal (Ctrl+C / SIGTERM)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	// 6. Graceful shutdown with 10s timeout budget
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	app.Stop(shutdownCtx)
}
