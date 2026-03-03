// Package main initializes and runs the Heimdall Data Plane service.
//
// It acts as the composition root for the high-performance gRPC API,
// wiring up the Redis cache (L2), In-Memory cache (L1), Rule Engine,
// and handling the server lifecycle.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	"github.com/redis/go-redis/v9"

	"github.com/rafaeljc/heimdall/internal/cache"
	"github.com/rafaeljc/heimdall/internal/config"
	"github.com/rafaeljc/heimdall/internal/dataapi"
	"github.com/rafaeljc/heimdall/internal/logger"
	"github.com/rafaeljc/heimdall/internal/observability"
	"github.com/rafaeljc/heimdall/internal/ruleengine"
	"github.com/rafaeljc/heimdall/internal/security"
)

// Infrastructure holds all initialized components for the data plane.
// It represents the composition of caches (L1 memory, L2 Redis), API, and observability
// layers required for the high-performance gRPC service.
type Infrastructure struct {
	RedisClient *redis.Client
	API         *dataapi.API
	ObsServer   *observability.Server
}

// Service manages the data plane gRPC lifecycle.
type Service struct {
	cfg        *config.Config
	log        *slog.Logger
	infra      *Infrastructure
	grpcServer *grpc.Server
	listener   net.Listener
	errChan    chan error // Buffered with capacity 1; async server errors are sent here.
}

// NewService creates and initializes a Service instance.
func NewService(ctx context.Context, cfg *config.Config, log *slog.Logger) (*Service, error) {
	// Initialize infrastructure
	infra, err := initializeInfrastructure(ctx, cfg, log)
	if err != nil {
		return nil, err
	}

	// Create listener
	listener, err := net.Listen("tcp", ":"+cfg.Server.Data.Port)
	if err != nil {
		// Clean up infrastructure on listener creation failure to prevent resource leaks
		infra.API.Close()
		infra.RedisClient.Close()
		return nil, fmt.Errorf("failed to bind port %s: %w", cfg.Server.Data.Port, err)
	}

	// Define Server Options
	opts := []grpc.ServerOption{
		infra.API.InterceptorChain,
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:                  cfg.Server.Data.KeepaliveTime,
			Timeout:               cfg.Server.Data.KeepaliveTimeout,
			MaxConnectionAge:      cfg.Server.Data.MaxConnectionAge,
			MaxConnectionAgeGrace: cfg.Server.Data.MaxConnectionAgeGrace,
			MaxConnectionIdle:     cfg.Server.Data.MaxConnectionIdle,
		}),
		grpc.MaxRecvMsgSize(cfg.Server.Data.MaxRecvMsgSize),
		grpc.MaxSendMsgSize(cfg.Server.Data.MaxSendMsgSize),
		grpc.MaxConcurrentStreams(cfg.Server.Data.MaxConcurrentStreams),
	}

	// Configure TLS if enabled.
	// On error, clean up all resources including the listener to prevent resource leaks.
	if cfg.Server.Data.TLSEnabled {
		loader, err := security.NewTLSLoader(cfg.Server.Data.TLSCert, cfg.Server.Data.TLSKey)
		if err != nil {
			infra.API.Close()
			infra.RedisClient.Close()
			listener.Close()
			return nil, fmt.Errorf("failed to load TLS credentials: %w", err)
		}

		tlsConfig, err := loader.LoadConfig()
		if err != nil {
			infra.API.Close()
			infra.RedisClient.Close()
			listener.Close()
			return nil, fmt.Errorf("failed to configure TLS: %w", err)
		}

		creds := credentials.NewTLS(tlsConfig)
		opts = append(opts, grpc.Creds(creds))
		log.Info("grpc tls enabled", slog.String("cert_file", cfg.Server.Data.TLSCert))
	}

	grpcServer := grpc.NewServer(opts...)
	infra.API.Register(grpcServer)

	if cfg.App.Environment != config.EnvironmentProduction {
		// Reflection is non-critical but useful for debugging in non-production environments.
		reflection.Register(grpcServer)
		log.Info("grpc reflection enabled")
	}

	return &Service{
		cfg:        cfg,
		log:        log,
		infra:      infra,
		grpcServer: grpcServer,
		listener:   listener,
		errChan:    make(chan error, 1),
	}, nil
}

// Start begins serving the gRPC server.
func (s *Service) Start() {
	s.log.Info("server listening", slog.String("address", s.listener.Addr().String()))

	go func() {
		if err := s.grpcServer.Serve(s.listener); err != nil {
			s.errChan <- fmt.Errorf("failed to serve grpc: %w", err)
		}
	}()
}

// ErrorChan returns the channel for receiving server errors.
func (s *Service) ErrorChan() <-chan error {
	return s.errChan
}

// Shutdown gracefully stops the service.
// It closes all resources in parallel (gRPC server and observability server),
// respecting the shutdown timeout context. Returns an error if any resource
// fails to shutdown or if the shutdown timeout is exceeded.
func (s *Service) Shutdown(ctx context.Context) error {
	var wg sync.WaitGroup
	errChan := make(chan error, 2)

	// Close gRPC server with graceful shutdown
	wg.Add(1)
	go func() {
		defer wg.Done()
		stopped := make(chan struct{})
		go func() {
			s.grpcServer.GracefulStop()
			close(stopped)
		}()

		select {
		case <-stopped:
			s.log.Info("grpc server stopped gracefully")
		case <-ctx.Done():
			s.grpcServer.Stop()
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
// It closes cache connections without waiting for in-flight RPCs.
func (s *Service) Close() {
	s.log.Info("releasing api resources")
	s.infra.API.Close()

	s.log.Info("closing redis connection")
	if err := s.infra.RedisClient.Close(); err != nil {
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

// run executes the service lifecycle: configuration, logger setup, initialization, server startup, and graceful shutdown.
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

	// Set Global Default.
	// This ensures that:
	// 1. The Interceptor uses the correct format when creating child loggers.
	// 2. Any library using slog defaults respects our config.
	slog.SetDefault(log)

	log.Info("starting service",
		slog.String("port", cfg.Server.Data.Port),
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
	svc.Start()

	// -------------------------------------------------------------------------
	// 2. Execution & Graceful Shutdown
	// -------------------------------------------------------------------------

	// Create a channel to listen for OS interrupt signals (Ctrl+C, SIGTERM).
	// Block and wait for either a server error or an OS signal to stop.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-svc.ErrorChan():
		return err
	case sig := <-sigChan:
		log.Info("shutdown signal received", slog.String("signal", sig.String()))
	}

	// Create timeout context for shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.App.ShutdownTimeout)
	defer cancel()

	if err := svc.Shutdown(shutdownCtx); err != nil {
		return err
	}

	log.Info("service exited successfully")
	return nil
}

// initializeInfrastructure sets up caches, API, and observability server.
// Note: The data plane does not monitor the database because it is a read-only cache layer
// with no direct database dependencies. Database health is monitored by the control plane.
func initializeInfrastructure(ctx context.Context, cfg *config.Config, log *slog.Logger) (*Infrastructure, error) {
	memoryCache, err := cache.NewMemoryCache(cfg.Server.Data.L1CacheCapacity, cfg.Server.Data.L1CacheTTL)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize L1 cache: %w", err)
	}

	engine := ruleengine.New(log)

	redisClient, err := cache.NewRedisClient(ctx, &cfg.Redis)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	go cache.RunPoolMonitor(ctx, redisClient, cfg.Observability.MetricsPollingInterval)
	go memoryCache.RunMetricsMonitor(ctx, cfg.Observability.MetricsPollingInterval)

	redisCache := cache.NewRedisCache(redisClient)

	api, err := dataapi.NewAPI(log, memoryCache, redisCache, engine, cfg.Server.Data.APIKeyHash)
	if err != nil {
		memoryCache.Close()
		redisClient.Close()
		return nil, fmt.Errorf("failed to initialize data api: %w", err)
	}

	obsServer := observability.NewServer(
		log,
		&cfg.Observability,
		cache.NewHealthChecker(redisClient),
	)
	obsServer.Start()

	return &Infrastructure{
		RedisClient: redisClient,
		API:         api,
		ObsServer:   obsServer,
	}, nil
}
