// Package dataapi implements the gRPC Data Plane for flag evaluation.
// It handles the high-performance read path for client SDKs.
package dataapi

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/rafaeljc/heimdall/internal/cache"
	"github.com/rafaeljc/heimdall/internal/config"
	"github.com/rafaeljc/heimdall/internal/logger"
	"github.com/rafaeljc/heimdall/internal/observability"
	"github.com/rafaeljc/heimdall/internal/ruleengine"
	pb "github.com/rafaeljc/heimdall/proto/heimdall/v1"
	"google.golang.org/grpc"
)

// API implements the gRPC DataPlane service defined in the proto contract.
// It embeds the UnimplementedDataPlaneServer for forward compatibility.
type API struct {
	pb.UnimplementedDataPlaneServer

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
}

// NewAPI creates a new Data Plane gRPC API instance.
func NewAPI(cfg *config.DataPlaneConfig, log *slog.Logger, l2 cache.Service, engine *ruleengine.Engine) (*API, error) {
	if cfg == nil {
		panic("dataapi: config cannot be nil")
	}
	if l2 == nil {
		panic("dataapi: cache service cannot be nil")
	}

	// Initialize L1 Cache (Otter) using configuration
	l1, err := cache.NewMemoryCache(cfg.L1CacheCapacity, cfg.L1CacheTTL)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize l1 cache: %w", err)
	}

	// We inject the system logger into the backgound context so that
	// internal tasks (like watchUpdates) can log without needing a struct field.
	bgCtx := logger.WithContext(context.Background(), log)
	ctx, cancel := context.WithCancel(bgCtx)

	api := &API{
		l1:     l1,
		l2:     l2,
		engine: engine,
		ctx:    ctx,
		cancel: cancel,
	}

	// -------------------------------------------------------------------------
	// Background Tasks
	// -------------------------------------------------------------------------

	// 1. Start Pub/Sub Invalidation Watcher
	api.wg.Add(1)
	go api.watchUpdates()

	// 2. Start Async Metrics Collector for L1 Cache
	api.wg.Add(1)
	go func() {
		defer api.wg.Done()
		api.l1.RunMetricsCollector(api.ctx, 0) // Default interval
	}()

	return api, nil
}

// Register connects this implementation to the grpc.Server engine.
// This encapsulates the registration logic, keeping main.go clean.
func (a *API) Register(grpcServer *grpc.Server) {
	pb.RegisterDataPlaneServer(grpcServer, a)
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
