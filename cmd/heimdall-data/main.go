// Package main initializes and runs the Heimdall Data Plane service.
//
// It acts as the composition root for the high-performance gRPC API,
// wiring up the Redis cache and handling the server lifecycle.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/rafaeljc/heimdall/internal/cache"
	"github.com/rafaeljc/heimdall/internal/dataapi"
)

// main is the application entrypoint.
func main() {
	if err := run(); err != nil {
		log.Printf("Fatal error: %v", err)
		os.Exit(1)
	}
}

// run executes the service lifecycle.
func run() error {
	appName := "heimdall-data-plane"
	port := "50051" // Standard gRPC port

	log.Printf("Starting %s service...", appName)

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

	// Initialize the gRPC Server
	// In the future, we can add Interceptors here (Logging, Auth, Metrics)
	grpcServer := grpc.NewServer()

	// Register our implementation with the server engine
	api.Register(grpcServer)

	// Enable Server Reflection.
	// This allows tools like 'grpcurl' or Postman to inspect the API
	// dynamically without needing the .proto file locally.
	reflection.Register(grpcServer)

	log.Printf("gRPC Server listening on port %s", port)

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
	case <-sigChan:
		log.Println("Shutdown signal received. Stopping gRPC server...")
	}

	// GracefulStop waits for pending RPCs to finish before closing.
	// Unlike HTTP, it doesn't take a Context/Timeout, it blocks until done.
	// For a more robust implementation, we could wrap this in a timeout select.
	grpcServer.GracefulStop()

	log.Println("Service exited successfully")
	return nil
}
