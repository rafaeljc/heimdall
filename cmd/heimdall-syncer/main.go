// Package main initializes and runs the Heimdall Syncer worker.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rafaeljc/heimdall/internal/cache"
	"github.com/rafaeljc/heimdall/internal/database"
	"github.com/rafaeljc/heimdall/internal/logger"
	"github.com/rafaeljc/heimdall/internal/store"
	"github.com/rafaeljc/heimdall/internal/syncer"
)

func main() {
	if err := run(); err != nil {
		slog.Error("service exited with fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	appName := "heimdall-syncer"

	// -------------------------------------------------------------------------
	// 0. Logger Setup
	// -------------------------------------------------------------------------
	logConfig := logger.NewConfig(
		appName,
		os.Getenv("APP_ENV"),
		os.Getenv("LOG_LEVEL"),
	)

	logger.Setup(logConfig)

	slog.Info("starting service")

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

	// Create a background context that we can cancel on shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// -------------------------------------------------------------------------
	// 2. Infrastructure Setup
	// -------------------------------------------------------------------------

	// Initialize Postgres Pool
	pgPool, err := database.NewPostgresPool(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}
	defer pgPool.Close()

	// Initialize Redis Client
	redisCache, err := cache.NewRedisCache(ctx, redisURL)
	if err != nil {
		return fmt.Errorf("failed to connect to redis: %w", err)
	}
	defer redisCache.Close()

	// -------------------------------------------------------------------------
	// 3. Dependency Injection
	// -------------------------------------------------------------------------

	// Layer 1: Data Access
	flagRepo := store.NewPostgresStore(pgPool)

	// Layer 2: Service Logic
	worker := syncer.New(
		syncer.Config{
			Interval: 10 * time.Second, // Poll every 10s
		},
		flagRepo,
		redisCache,
	)

	// -------------------------------------------------------------------------
	// 4. Execution & Graceful Shutdown
	// -------------------------------------------------------------------------

	// Create a channel for errors coming from the worker
	errChan := make(chan error, 1)

	// Start worker in background
	go func() {
		if err := worker.Run(ctx); err != nil {
			errChan <- err
		}
	}()

	// Wait for OS signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errChan:
		return fmt.Errorf("worker crashed: %w", err)
	case <-sigChan:
		slog.Info("shutdown signal received")
		cancel() // Cancels the context passed to worker.Run(), stopping the loop
	}

	// Give some time for cleanup if needed (though context cancel is usually enough)
	time.Sleep(1 * time.Second)
	slog.Info("service exited successfully")
	return nil
}
