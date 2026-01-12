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
	"github.com/rafaeljc/heimdall/internal/config"
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
	// -------------------------------------------------------------------------
	// 0. Configuration
	// -------------------------------------------------------------------------
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// -------------------------------------------------------------------------
	// 1. Logger Setup
	// -------------------------------------------------------------------------
	log := logger.New(&cfg.App)

	// Set Global Default
	// Crucial so that 'slog.Info' calls within this file use the correct format
	// and libraries that rely on global slog conform to our standard.
	slog.SetDefault(log)

	log.Info("starting service")

	// Create a background context that we can cancel on shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// -------------------------------------------------------------------------
	// 2. Infrastructure Setup
	// -------------------------------------------------------------------------

	// Initialize Postgres Pool
	pgPool, err := database.NewPostgresPool(ctx, cfg.Database.ConnectionString())
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}
	defer pgPool.Close()

	// Initialize Redis Client
	redisCache, err := cache.NewRedisCache(ctx, &cfg.Redis)
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
		log,
		syncer.Config{}, // Use defaults
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
	case sig := <-sigChan:
		log.Info("shutdown signal received", slog.String("signal", sig.String()))
		cancel() // Cancels the context passed to worker.Run(), stopping the loop
	}

	// Give some time for cleanup if needed
	// (The worker should return quickly after context cancellation)
	time.Sleep(500 * time.Millisecond)

	log.Info("service exited successfully")
	return nil
}
