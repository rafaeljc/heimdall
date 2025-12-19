// Package main initializes and runs the Heimdall Control Plane service.
//
// It acts as the composition root, wiring up the database, repository layer,
// and REST API, while managing the application lifecycle and graceful shutdown.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rafaeljc/heimdall/internal/cache"
	"github.com/rafaeljc/heimdall/internal/controlapi"
	"github.com/rafaeljc/heimdall/internal/database"
	"github.com/rafaeljc/heimdall/internal/logger"
	"github.com/rafaeljc/heimdall/internal/store"
)

// main is the application entrypoint.
// It delegates execution to the run() function and handles the process exit code
// based on the returned error to ensure proper integration with container orchestrators.
func main() {
	if err := run(); err != nil {
		slog.Error("service exited with fatal error", "error", err)
		os.Exit(1)
	}
}

// run executes the service lifecycle: initialization, server startup, and graceful shutdown.
// It returns an error instead of exiting directly, allowing deferred functions
// (like database cleanup) to execute properly before the process terminates.
func run() error {
	appName := "heimdall-control-plane"
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	appEnv := os.Getenv("APP_ENV")
	logLevel := os.Getenv("LOG_LEVEL")

	// -------------------------------------------------------------------------
	// 0. Logger Setup
	// -------------------------------------------------------------------------
	logCfg := logger.NewConfig(appName, appEnv, logLevel)

	// Create the logger instance
	log := logger.New(logCfg)

	// Set Global Default.
	// This ensures that:
	// 1. All slog.Info/Error calls in this file use the configured format (JSON/Text).
	// 2. The HTTP Middleware (controlapi) can derive child loggers from this default.
	slog.SetDefault(log)

	log.Info("starting service",
		slog.String("port", port),
		slog.String("env", string(logCfg.Environment)),
	)

	// -------------------------------------------------------------------------
	// 1. Configuration
	// -------------------------------------------------------------------------
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return fmt.Errorf("DATABASE_URL environment variable is required")
	}
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		return fmt.Errorf("REDIS_URL environment variable is required")
	}

	// Create a background context for the initialization phase
	ctx := context.Background()

	// Initialize the DB Pool.
	pgPool, err := database.NewPostgresPool(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("could not connect to database: %w", err)
	}
	defer pgPool.Close()

	// Initialize Redis Client
	redisCache, err := cache.NewRedisCache(ctx, redisURL)
	if err != nil {
		return fmt.Errorf("failed to connect to redis: %w", err)
	}
	defer redisCache.Close()

	// -------------------------------------------------------------------------
	// 2. Dependency Injection & Wiring
	// -------------------------------------------------------------------------

	// Layer 1: Data Access (Repository)
	// We pass the pgPool instance explicitly.
	flagStore := store.NewPostgresStore(pgPool)

	// Layer 2: API (Controller)
	// Inject the repository into the API handler.
	api := controlapi.NewAPI(flagStore, redisCache)

	// -------------------------------------------------------------------------
	// 3. HTTP Server Setup
	// -------------------------------------------------------------------------

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           api.Router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Create the Listener explicitly before starting the server.
	// This allows us to validate that the port is available immediately and
	// log the "Listening" message with confidence.
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("failed to bind port %s: %w", port, err)
	}

	log.Info("server listening", slog.String("address", listener.Addr().String()))

	// Start the HTTP server in a separate goroutine so it doesn't block the main thread.
	// We use a buffered error channel to capture any startup failures (e.g., port closed after bind).
	errChan := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("failed to serve: %w", err)
		}
	}()

	// -------------------------------------------------------------------------
	// 4. Graceful Shutdown
	// -------------------------------------------------------------------------

	// Create a channel to listen for OS interrupt signals (Ctrl+C, SIGTERM).
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Block and wait for either:
	// 1. A fatal server error (via errChan)
	// 2. An OS signal to stop (via sigChan)
	select {
	case err := <-errChan:
		return err
	case sig := <-sigChan:
		log.Info("shutdown signal received", slog.String("signal", sig.String()))
	}

	// Create a timeout context to force shutdown after 5 seconds if
	// pending requests do not finish in time.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server forced to shutdown: %w", err)
	}

	log.Info("service exited successfully")
	return nil
}
