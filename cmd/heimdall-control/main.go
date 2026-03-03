// Package main initializes and runs the Heimdall Control Plane service.
//
// It acts as the composition root, wiring up the database, repository layer,
// and REST API, while managing the application lifecycle and graceful shutdown.
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/rafaeljc/heimdall/internal/cache"
	"github.com/rafaeljc/heimdall/internal/config"
	"github.com/rafaeljc/heimdall/internal/controlapi"
	"github.com/rafaeljc/heimdall/internal/database"
	"github.com/rafaeljc/heimdall/internal/logger"
	"github.com/rafaeljc/heimdall/internal/observability"
	"github.com/rafaeljc/heimdall/internal/security"
	"github.com/rafaeljc/heimdall/internal/store"
)

// Infrastructure holds all initialized components for the control plane.
// It represents the composition of database, cache, REST API, and observability layers
// required for the control plane to manage system state.
type Infrastructure struct {
	DB        *pgxpool.Pool
	Redis     *redis.Client
	API       *controlapi.API
	ObsServer *observability.Server
}

// Service manages the control plane lifecycle.
type Service struct {
	cfg      *config.Config
	log      *slog.Logger
	infra    *Infrastructure
	server   *http.Server
	listener net.Listener
	errChan  chan error // Buffered with capacity 1; async server errors are sent here.
}

// NewService creates and initializes a Service instance.
func NewService(ctx context.Context, cfg *config.Config, log *slog.Logger) (*Service, error) {
	// Initialize infrastructure
	infra, err := initializeInfrastructure(ctx, cfg, log)
	if err != nil {
		return nil, err
	}

	// Configure TLS if enabled (before server creation for consistency with gRPC)
	var tlsConfig *tls.Config
	if cfg.Server.Control.TLSEnabled {
		loader, err := security.NewTLSLoader(cfg.Server.Control.TLSCert, cfg.Server.Control.TLSKey)
		if err != nil {
			infra.DB.Close()
			infra.Redis.Close()
			return nil, fmt.Errorf("failed to load TLS credentials: %w", err)
		}

		tlsConfig, err = loader.LoadConfig()
		if err != nil {
			infra.DB.Close()
			infra.Redis.Close()
			return nil, fmt.Errorf("failed to configure TLS: %w", err)
		}
	}

	// Setup HTTP server with TLS config
	server := &http.Server{
		Addr:              ":" + cfg.Server.Control.Port,
		Handler:           infra.API.Router,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: cfg.Server.Control.ReadHeaderTimeout,
		ReadTimeout:       cfg.Server.Control.ReadTimeout,
		WriteTimeout:      cfg.Server.Control.WriteTimeout,
		IdleTimeout:       cfg.Server.Control.IdleTimeout,
	}

	// Create listener
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		infra.DB.Close()
		infra.Redis.Close()
		return nil, fmt.Errorf("failed to bind port %s: %w", cfg.Server.Control.Port, err)
	}

	return &Service{
		cfg:      cfg,
		log:      log,
		infra:    infra,
		server:   server,
		listener: listener,
		errChan:  make(chan error, 1),
	}, nil
}

// Start begins serving the HTTP server.
func (s *Service) Start() error {
	s.log.Info("server listening", slog.String("address", s.listener.Addr().String()))

	if s.cfg.Server.Control.TLSEnabled {
		s.log.Info("https enabled", slog.String("cert_file", s.cfg.Server.Control.TLSCert))

		go func() {
			if err := s.server.ServeTLS(s.listener, "", ""); err != nil && err != http.ErrServerClosed {
				s.errChan <- fmt.Errorf("failed to serve https: %w", err)
			}
		}()
	} else {
		go func() {
			if err := s.server.Serve(s.listener); err != nil && err != http.ErrServerClosed {
				s.errChan <- fmt.Errorf("failed to serve http: %w", err)
			}
		}()
	}

	return nil
}

// ErrorChan returns the channel for receiving server errors.
func (s *Service) ErrorChan() <-chan error {
	return s.errChan
}

// Shutdown gracefully stops the service.
// It closes all resources in parallel (HTTP server and observability server),
// respecting the shutdown timeout context. Returns an error if any resource
// fails to shutdown or if the shutdown timeout is exceeded.
func (s *Service) Shutdown(ctx context.Context) error {
	var wg sync.WaitGroup
	errChan := make(chan error, 2)

	// Close HTTP server
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := s.server.Shutdown(ctx); err != nil {
			errChan <- fmt.Errorf("server shutdown error: %w", err)
		}
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

// run executes the service lifecycle: initialization, server startup, and graceful shutdown.
// It returns an error instead of exiting directly, allowing deferred functions
// (like database cleanup) to execute properly before the process terminates.
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
	// Create the logger instance using config
	log := logger.New(&cfg.App)

	// Set Global Default.
	// This ensures that:
	// 1. All slog.Info/Error calls in this file use the configured format (JSON/Text).
	// 2. The HTTP Middleware (controlapi) can derive child loggers from this default.
	slog.SetDefault(log)

	log.Info("starting service",
		slog.String("port", cfg.Server.Control.Port),
		slog.String("env", cfg.App.Environment),
	)

	// Create a background context for initialization.
	// This context is used to set up connections and is not cancelled during normal operation.
	ctx := context.Background()

	// Initialize service
	svc, err := NewService(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer svc.Close()

	// Start the service
	if err := svc.Start(); err != nil {
		return err
	}

	// -------------------------------------------------------------------------
	// 2. Execution & Graceful Shutdown
	// -------------------------------------------------------------------------

	// Create a channel to listen for OS interrupt signals (Ctrl+C, SIGTERM).
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Block and wait for either:
	// 1. A fatal server error (via svc.ErrorChan)
	// 2. An OS signal to stop (via sigChan)
	select {
	case err := <-svc.ErrorChan():
		return err
	case sig := <-sigChan:
		log.Info("shutdown signal received", slog.String("signal", sig.String()))
	}

	// Create a timeout context to force shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.App.ShutdownTimeout)
	defer cancel()

	if err := svc.Shutdown(shutdownCtx); err != nil {
		return err
	}

	log.Info("service exited successfully")
	return nil
}

// initializeInfrastructure sets up database, Redis, API, and observability server.
func initializeInfrastructure(ctx context.Context, cfg *config.Config, log *slog.Logger) (*Infrastructure, error) {
	// Initialize the DB Pool
	pgPool, err := database.NewPostgresPool(ctx, &cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
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

	// Layer 1: Data Access (Repository)
	flagStore := store.NewPostgresStore(pgPool)
	redisCache := cache.NewRedisCache(redisClient)

	// Layer 2: API (Controller)
	api := controlapi.NewAPI(flagStore, redisCache, cfg.Server.Control.APIKeyHash)

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
		API:       api,
		ObsServer: obsServer,
	}, nil
}
