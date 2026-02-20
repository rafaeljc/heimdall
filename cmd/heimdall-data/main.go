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
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	"github.com/rafaeljc/heimdall/internal/cache"
	"github.com/rafaeljc/heimdall/internal/config"
	"github.com/rafaeljc/heimdall/internal/dataapi"
	"github.com/rafaeljc/heimdall/internal/logger"
	"github.com/rafaeljc/heimdall/internal/observability"
	"github.com/rafaeljc/heimdall/internal/ruleengine"
	"github.com/rafaeljc/heimdall/internal/security"
)

// main is the application entrypoint.
func main() {
	if err := run(); err != nil {
		slog.Error("service exited with fatal error", "error", err)
		os.Exit(1)
	}
}

// run executes the service lifecycle.
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

	// Background context for initialization
	ctx := context.Background()

	// -------------------------------------------------------------------------
	// 2. Infrastructure Setup
	// -------------------------------------------------------------------------

	// Initialize In-Memory Cache (L1 Cache)
	memoryCache, err := cache.NewMemoryCache(cfg.Server.Data.L1CacheCapacity, cfg.Server.Data.L1CacheTTL)
	if err != nil {
		return fmt.Errorf("failed to initialize l1 cache: %w", err)
	}

	// Rule Engine
	engine := ruleengine.New(log)

	// Initialize Redis Client (L2 Cache)
	redisClient, err := cache.NewRedisClient(ctx, &cfg.Redis)
	if err != nil {
		return fmt.Errorf("failed to connect to redis: %w", err)
	}
	// Deferred close happens in reverse order of creation in the shutdown block
	// but we place it here conceptually. We will manage exact closing order below.

	// Start monitors
	go cache.RunPoolMonitor(ctx, redisClient, cfg.Observability.MetricsPollingInterval)
	go memoryCache.RunMetricsMonitor(ctx, cfg.Observability.MetricsPollingInterval)

	// -------------------------------------------------------------------------
	// 3. Wiring (Dependency Injection)
	// -------------------------------------------------------------------------
	redisCache := cache.NewRedisCache(redisClient)

	// Initialize the gRPC API implementation
	api, err := dataapi.NewAPI(log, memoryCache, redisCache, engine, cfg.Server.Data.APIKeyHash)
	if err != nil {
		redisClient.Close()
		return fmt.Errorf("failed to initialize data api: %w", err)
	}

	// Observability Server Initialization
	obsServer := observability.NewServer(
		log,
		&cfg.Observability,
		cache.NewHealthChecker(redisClient),
	)
	obsServer.Start()

	// -------------------------------------------------------------------------
	// 4. gRPC Server Setup
	// -------------------------------------------------------------------------

	// Create the TCP listener first (Fail Fast)
	listener, err := net.Listen("tcp", ":"+cfg.Server.Data.Port)
	if err != nil {
		api.Close()
		redisClient.Close()
		return fmt.Errorf("failed to bind port %s: %w", cfg.Server.Data.Port, err)
	}

	// Define Server Options
	opts := []grpc.ServerOption{
		api.InterceptorChain,
		// Keepalive and connection lifecycle settings
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:                  cfg.Server.Data.KeepaliveTime,
			Timeout:               cfg.Server.Data.KeepaliveTimeout,
			MaxConnectionAge:      cfg.Server.Data.MaxConnectionAge,
			MaxConnectionAgeGrace: cfg.Server.Data.MaxConnectionAgeGrace,
			MaxConnectionIdle:     cfg.Server.Data.MaxConnectionIdle,
		}),
		// Resource limits
		grpc.MaxRecvMsgSize(cfg.Server.Data.MaxRecvMsgSize),
		grpc.MaxSendMsgSize(cfg.Server.Data.MaxSendMsgSize),
		grpc.MaxConcurrentStreams(cfg.Server.Data.MaxConcurrentStreams),
	}

	// Configure TLS if enabled
	if cfg.Server.Data.TLSEnabled {
		loader, err := security.NewTLSLoader(
			cfg.Server.Data.TLSCert,
			cfg.Server.Data.TLSKey,
		)
		if err != nil {
			api.Close()
			redisClient.Close()
			return fmt.Errorf("failed to load TLS credentials: %w", err)
		}

		tlsConfig, err := loader.LoadConfig()
		if err != nil {
			api.Close()
			redisClient.Close()
			return fmt.Errorf("failed to configure TLS: %w", err)
		}

		creds := credentials.NewTLS(tlsConfig)
		opts = append(opts, grpc.Creds(creds))
		log.Info("grpc tls enabled", slog.String("cert_file", cfg.Server.Data.TLSCert))
	}

	// Initialize the gRPC Server
	grpcServer := grpc.NewServer(opts...)

	// Register our implementation with the server engine
	api.Register(grpcServer)

	// Enable Server Reflection.
	// This allows tools like 'grpcurl' or Postman to inspect the API
	// dynamically without needing the .proto file locally.
	if cfg.App.Environment != config.EnvironmentProduction {
		reflection.Register(grpcServer)
		log.Info("grpc reflection enabled")
	}

	log.Info("server listening", slog.String("address", listener.Addr().String()))

	// Start serving in a goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			errChan <- fmt.Errorf("failed to serve grpc: %w", err)
		}
	}()

	// -------------------------------------------------------------------------
	// 5. Graceful Shutdown
	// -------------------------------------------------------------------------

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errChan:
		return err
	case sig := <-sigChan:
		log.Info("shutdown signal received", slog.String("signal", sig.String()))
	}

	// Graceful Shutdown with Timeout
	// gRPC GracefulStop waits indefinitely for active RPCs. We enforce a limit.
	stopped := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(stopped)
	}()
	t := time.NewTimer(cfg.App.ShutdownTimeout)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.App.ShutdownTimeout)
	defer cancel()

	if err := obsServer.Shutdown(shutdownCtx); err != nil {
		log.Warn("observability server shutdown error", slog.String("error", err.Error()))
	}

	select {
	case <-stopped:
		log.Info("grpc server stopped gracefully")
	case <-t.C:
		log.Warn("shutdown timed out, forcing stop")
		grpcServer.Stop()
	}

	log.Info("releasing api resources")
	api.Close()

	log.Info("closing redis connection")
	if err := redisClient.Close(); err != nil {
		log.Error("error closing redis", slog.String("error", err.Error()))
	}

	log.Info("shutdown complete")
	return nil
}
