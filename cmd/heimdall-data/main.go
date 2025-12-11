// Package main initializes and runs the Heimdall Data Plane service.
//
// It acts as the composition root for the high-performance gRPC API,
// wiring up the Redis cache and handling the server lifecycle.
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
	"google.golang.org/grpc/reflection"

	"github.com/rafaeljc/heimdall/internal/cache"
	"github.com/rafaeljc/heimdall/internal/dataapi"
	"github.com/rafaeljc/heimdall/internal/logger"
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
	appName := "heimdall-data-plane"
	port := os.Getenv("PORT")
	if port == "" {
		port = "50051"
	}
	appEnv := os.Getenv("APP_ENV")
	logLevel := os.Getenv("LOG_LEVEL")

	// -------------------------------------------------------------------------
	// 0. Logger Setup
	// -------------------------------------------------------------------------
	logCfg := logger.NewConfig(appName, appEnv, logLevel)

	log := logger.New(logCfg)

	// Set Global Default.
	// This ensures that:
	// 1. The Interceptor uses the correct format when creating child loggers.
	// 2. Any library using slog defaults respects our config.
	slog.SetDefault(log)

	log.Info("starting service",
		slog.String("port", port),
		slog.String("env", string(logCfg.Environment)),
	)

	// -------------------------------------------------------------------------
	// 1. Configuration
	// -------------------------------------------------------------------------
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		return fmt.Errorf("REDIS_URL environment variable is required")
	}

	// Background context for initialization
	ctx := context.Background()

	// -------------------------------------------------------------------------
	// 2. Infrastructure Setup
	// -------------------------------------------------------------------------

	// Initialize Redis Client (L2 Cache)
	redisCache, err := cache.NewRedisCache(ctx, redisURL)
	if err != nil {
		return fmt.Errorf("failed to connect to redis: %w", err)
	}
	defer redisCache.Close()

	// -------------------------------------------------------------------------
	// 3. Wiring (Dependency Injection)
	// -------------------------------------------------------------------------

	// Initialize the gRPC API implementation
	api := dataapi.NewAPI(redisCache)

	// -------------------------------------------------------------------------
	// 4. gRPC Server Setup
	// -------------------------------------------------------------------------

	// Create the TCP listener first (Fail Fast)
	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return fmt.Errorf("failed to bind port %s: %w", port, err)
	}

	// Define Server Options (Interceptors)
	opts := []grpc.ServerOption{
		// Chain the logging interceptor.
		// This generates the Request ID and injects the logger into the Context.
		grpc.UnaryInterceptor(dataapi.RequestLoggerInterceptor()),
	}

	// Initialize the gRPC Server
	grpcServer := grpc.NewServer(opts...)

	// Register our implementation with the server engine
	api.Register(grpcServer)

	// Enable Server Reflection.
	// This allows tools like 'grpcurl' or Postman to inspect the API
	// dynamically without needing the .proto file locally.
	reflection.Register(grpcServer)

	log.Info("server listening", slog.String("address", listener.Addr().String()))

	// Start serving in a goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			errChan <- fmt.Errorf("failed to serve gRPC: %w", err)
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

	t := time.NewTimer(5 * time.Second)
	select {
	case <-stopped:
		log.Info("service exited successfully")
	case <-t.C:
		log.Warn("shutdown timed out, forcing stop")
		grpcServer.Stop()
	}

	return nil
}
