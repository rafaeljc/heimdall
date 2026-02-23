// Package main initializes and runs the Heimdall Syncer worker.
//
// The syncer is a background daemon that synchronizes flag data from the database
// to the Redis cache, ensuring data consistency across the distributed system.
// It manages its own lifecycle with graceful shutdown support.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/rafaeljc/heimdall/internal/cache"
	"github.com/rafaeljc/heimdall/internal/config"
	"github.com/rafaeljc/heimdall/internal/database"
	"github.com/rafaeljc/heimdall/internal/logger"
	"github.com/rafaeljc/heimdall/internal/observability"
	"github.com/rafaeljc/heimdall/internal/store"
	"github.com/rafaeljc/heimdall/internal/syncer"
)

// Infrastructure holds all initialized components for the syncer.
// It represents the composition of database, cache, worker, and observability layers
// required for the syncer to operate.
type Infrastructure struct {
	DB        *pgxpool.Pool
	Redis     *redis.Client
	Worker    *syncer.Service
	ObsServer *observability.Server
}

// Service manages the syncer lifecycle.
type Service struct {
	cfg     *config.Config
	log     *slog.Logger
	infra   *Infrastructure
	ctx     context.Context
	cancel  context.CancelFunc
	errChan chan error // Buffered with capacity 1; worker errors are sent here.
}

// NewService creates and initializes a Service instance.
// The parentCtx is typically context.Background(); the syncer operates independently
// and is stopped via signal handling, not context cancellation from above.
func NewService(parentCtx context.Context, cfg *config.Config, log *slog.Logger) (*Service, error) {
	// Create cancellable context for worker
	ctx, cancel := context.WithCancel(parentCtx)

	// Initialize infrastructure
	infra, err := initializeInfrastructure(ctx, cfg, log)
	if err != nil {
		cancel()
		return nil, err
	}

	return &Service{
		cfg:     cfg,
		log:     log,
		infra:   infra,
		ctx:     ctx,
		cancel:  cancel,
		errChan: make(chan error, 1),
	}, nil
}

// Start begins the worker.
func (s *Service) Start() {
	go func() {
		if err := s.infra.Worker.Run(s.ctx); err != nil {
			s.errChan <- err
		}
	}()
}

// ErrorChan returns the channel for receiving worker errors.
func (s *Service) ErrorChan() <-chan error {
	return s.errChan
}

// Stop signals the worker to stop by cancelling its context.
// This does not wait for the worker to finish; it initiates graceful termination.
func (s *Service) Stop() {
	s.cancel()
}

// Shutdown gracefully stops the service.
// It stops the worker and observability server in parallel,
// respecting the shutdown timeout context. Returns an error if any resource
// fails to shutdown or if the shutdown timeout is exceeded.
func (s *Service) Shutdown(ctx context.Context) error {
	var wg sync.WaitGroup
	errChan := make(chan error, 2)

	// Stop the worker
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.Stop()
		s.log.Info("syncer worker stopped")
	}()

	// Close observability server
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := s.infra.ObsServer.Shutdown(ctx); err != nil {
			errChan <- fmt.Errorf("observability server shutdown error: %w", err)
		}
	}()

	// Wait for all shutdowns to complete or timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	var timeoutError error
	select {
	case <-done:
		// All cleanups finished
	case <-ctx.Done():
		timeoutError = fmt.Errorf("shutdown timeout, some resources may not be fully cleaned")
	}

	// Collect any errors
	close(errChan)
	var errs []error
	for err := range errChan {
		if err != nil {
			errs = append(errs, err)
		}
	}

	// Include timeout error if it occurred
	if timeoutError != nil {
		errs = append(errs, timeoutError)
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}

	return nil
}

// Close releases all resources.
// This is a hard cleanup called at the end of the program lifecycle, not during graceful shutdown.
// It closes database and cache connections without waiting for in-flight operations.
func (s *Service) Close() {
	s.log.Info("closing database connection")
	s.infra.DB.Close()

	s.log.Info("closing redis connection")
	if err := s.infra.Redis.Close(); err != nil {
		s.log.Error("error closing redis", slog.String("error", err.Error()))
	}
}

// main is the application entrypoint.
// It delegates execution to the run() function and handles the process exit code
// based on the returned error to ensure proper integration with container orchestrators.
func main() {
	if err := run(); err != nil {
		slog.Error("service exited with fatal error", "error", err)
		os.Exit(1)
	}
}

// run executes the worker lifecycle: configuration, logger setup, initialization, execution, and graceful shutdown.
// It returns an error instead of exiting directly, allowing deferred functions to execute properly
// before the process terminates.
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

	// Create a background context for initialization
	ctx := context.Background()

	// Initialize service
	svc, err := NewService(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer svc.Close()

	// Start the worker
	svc.Start()

	// -------------------------------------------------------------------------
	// 2. Execution & Graceful Shutdown
	// -------------------------------------------------------------------------

	// Create a channel to listen for OS interrupt signals (Ctrl+C, SIGTERM).
	// Block and wait for either a worker error or an OS signal to stop.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-svc.ErrorChan():
		return fmt.Errorf("worker crashed: %w", err)
	case sig := <-sigChan:
		log.Info("shutdown signal received", slog.String("signal", sig.String()))
	}

	// Create timeout context for graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.App.ShutdownTimeout)
	defer cancel()

	if err := svc.Shutdown(shutdownCtx); err != nil {
		return err
	}

	log.Info("service exited successfully")
	return nil
}

// initializeInfrastructure sets up database, Redis, worker, and observability server.
func initializeInfrastructure(ctx context.Context, cfg *config.Config, log *slog.Logger) (*Infrastructure, error) {
	// Initialize Postgres Pool
	pgPool, err := database.NewPostgresPool(ctx, &cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	// Initialize Redis Client
	redisClient, err := cache.NewRedisClient(ctx, &cfg.Redis)
	if err != nil {
		pgPool.Close()
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	// Start monitors
	go database.RunPoolMonitor(ctx, pgPool, cfg.Observability.MetricsPollingInterval)
	go cache.RunPoolMonitor(ctx, redisClient, cfg.Observability.MetricsPollingInterval)

	// Layer 1: Data Access
	flagRepo := store.NewPostgresStore(pgPool)
	redisCache := cache.NewRedisCache(redisClient)

	// Layer 2: Service Logic
	worker := syncer.New(
		log,
		cfg.Syncer,
		flagRepo,
		redisCache,
	)

	// Start monitor
	go worker.RunQueueMonitor(ctx, cfg.Observability.MetricsPollingInterval)

	// Observability Server Initialization
	obsServer := observability.NewServer(
		log,
		&cfg.Observability,
		database.NewHealthChecker(pgPool),
		cache.NewHealthChecker(redisClient),
	)
	obsServer.Start()

	return &Infrastructure{
		DB:        pgPool,
		Redis:     redisClient,
		Worker:    worker,
		ObsServer: obsServer,
	}, nil
}
