// Package dataapi implements the gRPC Data Plane for flag evaluation.
// It handles the high-performance read path for client SDKs.
package dataapi

import (
	"context"
	"log/slog"
	"sync"

	"github.com/rafaeljc/heimdall/internal/cache"
	"github.com/rafaeljc/heimdall/internal/logger"
	"github.com/rafaeljc/heimdall/internal/observability"
	"github.com/rafaeljc/heimdall/internal/ruleengine"
	pb "github.com/rafaeljc/heimdall/proto/heimdall/v1"
	"google.golang.org/grpc"
)

// API implements the gRPC DataPlane service defined in the proto contract.
// It embeds the UnimplementedDataPlaneServiceServer for forward compatibility.
type API struct {
	pb.UnimplementedDataPlaneServiceServer
	InterceptorChain grpc.ServerOption

	l1     *cache.MemoryCache
	l2     cache.Service
	engine *ruleengine.Engine

	// Architecture Note:
	// We deliberately do NOT inject a *slog.Logger struct field here.
	// Since gRPC is request-scoped, handlers (in eval_handler.go) must retrieve
	// the logger from the context using logger.FromContext(ctx) to ensure
	// logs are correlated with the correct Request ID.
	//
	// For background tasks (like watchUpdates), we rely on the logger embedded
	// in the lifecycle context (ctx) below.

	// Lifecycle management
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup // Guarantees that all goroutines finish on shutdown

	// apiKeyHash is the SHA-256 hash of the valid API key.
	// Used for authentication in production environments.
	apiKeyHash string

	// skipAuth disables authentication when true (test/dev environments only).
	// Production environments should always set this to false.
	skipAuth bool
}

// NewAPI creates a new Data Plane gRPC API instance with authentication enabled.
//
// Parameters:
//
//	log: System logger for background tasks and lifecycle events.
//	l1: In-memory cache (L1) for low-latency flag lookups. Must not be nil.
//	l2: Redis-backed cache service (L2) for distributed cache coherency. Must not be nil.
//	engine: Rule evaluation engine for flag evaluation logic. Must not be nil.
//	apiKeyHash: SHA-256 hash of the valid API key for authentication.
//	            Must not be empty in production. Use NewAPIWithConfig with skipAuth=true
//	            for testing environments that require authentication bypass.
//
// Returns:
//
//	*API: Fully initialized data plane instance ready for gRPC registration.
//	error: Not currently used, but reserved for future validation errors.
//
// Panics:
//
//	If l1, l2, engine are nil or if apiKeyHash is empty.
//	Panics are intentional to fail fast in misconfigured deployments.
//
// Lifecycle:
//
//	The returned API instance starts background tasks immediately:
//	- Pub/Sub invalidation watcher (monitors flag update streams from control plane)
//	- These tasks are stopped gracefully via API.Close()
func NewAPI(log *slog.Logger, l1 *cache.MemoryCache, l2 cache.Service, engine *ruleengine.Engine, apiKeyHash string) (*API, error) {
	return NewAPIWithConfig(log, l1, l2, engine, apiKeyHash, false)
}

// NewAPIWithConfig creates a new Data Plane gRPC API instance with configurable authentication.
//
// This is an advanced constructor that allows disabling authentication for testing.
// For production use, prefer NewAPI which enforces authentication.
//
// Parameters:
//
//	log: System logger for background tasks and lifecycle events.
//	l1: In-memory cache (L1) for low-latency flag lookups. Must not be nil.
//	l2: Redis-backed cache service (L2) for distributed cache coherency. Must not be nil.
//	engine: Rule evaluation engine for flag evaluation logic. Must not be nil.
//	apiKeyHash: SHA-256 hash of the valid API key. Only validated if skipAuth=false.
//	            In test environments with skipAuth=true, can be empty string.
//	skipAuth: When true, disables Bearer token authentication. ONLY for testing.
//	          Production deployments MUST use skipAuth=false.
//
// Returns:
//
//	*API: Fully initialized data plane instance with authentication optionally disabled.
//	error: Not currently used, but reserved for future validation errors.
//
// Panics:
//
//	If l1, l2, engine are nil.
//	If skipAuth=false and apiKeyHash is empty (authentication required but no key provided).
//	Panics are intentional to fail fast in misconfigured deployments.
//
// Security Considerations:
//
//   - skipAuth=true MUST ONLY be used in development/test environments.
//   - Leaving authentication disabled in production exposes the data plane to unauthorized access.
//   - The apiKeyHash is compared using constant-time comparison (AuthInterceptor) to prevent
//     timing attacks on the API key validation.
//
// Lifecycle:
//
//	The returned API instance starts background tasks immediately:
//	- Pub/Sub invalidation watcher (monitors flag update streams from control plane)
//	- These tasks continue until API.Close() is called for graceful shutdown
func NewAPIWithConfig(log *slog.Logger, l1 *cache.MemoryCache, l2 cache.Service, engine *ruleengine.Engine, apiKeyHash string, skipAuth bool) (*API, error) {
	if l1 == nil {
		panic("dataapi: memory cache cannot be nil")
	}
	if l2 == nil {
		panic("dataapi: cache service cannot be nil")
	}
	if engine == nil {
		panic("dataapi: rule engine cannot be nil")
	}

	// Validate authentication configuration
	if !skipAuth && apiKeyHash == "" {
		panic("dataapi: apiKeyHash cannot be empty when authentication is enabled")
	}

	// We inject the system logger into the backgound context so that
	// internal tasks (like watchUpdates) can log without needing a struct field.
	bgCtx := logger.WithContext(context.Background(), log)
	ctx, cancel := context.WithCancel(bgCtx)

	api := &API{
		l1:         l1,
		l2:         l2,
		engine:     engine,
		ctx:        ctx,
		cancel:     cancel,
		apiKeyHash: apiKeyHash,
		skipAuth:   skipAuth,
	}

	api.configureInterceptors()

	// -------------------------------------------------------------------------
	// Background Tasks
	// -------------------------------------------------------------------------

	// Start Pub/Sub Invalidation Watcher
	api.wg.Add(1)
	go api.watchUpdates()

	return api, nil
}

// configureInterceptors sets up the gRPC interceptor chain for the API.
// The order of interceptors is important for correct logging and authentication.
// We use grpc.ChainUnaryInterceptor to combine multiple interceptors into one.
//
// Interceptor Order:
// 1. RequestLoggerInterceptor: Injects RequestID and Logger into context (outermost)
// 2. AuthInterceptor: Validates authentication (middle)
// 3. ObservabilityInterceptor: Collects metrics (innermost)
//
// This order ensures that authentication failures are properly logged with the RequestID.
func (a *API) configureInterceptors() {
	interceptors := []grpc.UnaryServerInterceptor{
		RequestLoggerInterceptor(), // Outer: Injects RequestID and Logger
	}

	if !a.skipAuth {
		interceptors = append(interceptors, AuthInterceptor(a.apiKeyHash)) // Middle: Validates authentication
	}

	interceptors = append(interceptors, ObservabilityInterceptor()) // Inner: Collects metrics

	a.InterceptorChain = grpc.ChainUnaryInterceptor(interceptors...)
}

// Register connects this implementation to the grpc.Server engine.
// This encapsulates the registration logic, keeping main.go clean.
func (a *API) Register(grpcServer *grpc.Server) {
	pb.RegisterDataPlaneServiceServer(grpcServer, a)
}

// Close performs a graceful shutdown of the API.
// It cancels background contexts, waits for goroutines to finish, and closes resources.
func (a *API) Close() {
	// Retrieve system logger from the stored context
	log := logger.FromContext(a.ctx)

	// Signal cancellation to background tasks
	a.cancel()

	// Wait for all goroutines to finish
	a.wg.Wait()

	// Close resources
	a.l1.Close()

	log.Info("api resources released")
}

// watchUpdates listens for cache invalidation events from the L2 provider (Redis).
// When an event is received, it purges the key from L1 memory to ensure consistency.
func (a *API) watchUpdates() {
	defer a.wg.Done()

	log := logger.FromContext(a.ctx)
	log.Info("starting pub/sub invalidadtion watcher")

	// SubscribeInvalidation handles reconnections internally.
	// Passing a.ctx ensures the subscription stops when Close() is called.
	ch := a.l2.SubscribeInvalidation(a.ctx)

	for {
		select {
		case <-a.ctx.Done():
			log.Info("stopping pub/sub invalidation watcher (context cancelled)")
			return
		case key, ok := <-ch:
			if !ok {
				log.Warn("invalidation channel closed unexpectedly, stopping watcher")
				return
			}

			// Reactive Invalidation: Remove from local memory.
			// The next read will force a fetch from L2 cache (Redis).
			a.l1.Del(key)
			// Count invalidation events.
			observability.DataPlaneInvalidations.Inc()
			log.Debug("invalidated l1 cache key", slog.String("key", key))
		}
	}
}
