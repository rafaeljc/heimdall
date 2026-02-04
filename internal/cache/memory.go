package cache

import (
	"context"
	"log/slog"
	"time"

	"github.com/maypok86/otter"
	"github.com/rafaeljc/heimdall/internal/logger"
	"github.com/rafaeljc/heimdall/internal/observability"
	"github.com/rafaeljc/heimdall/internal/ruleengine"
)

// MemoryCache acts as the L1 caching layer using a high-performance,
// contention-free algorithm (S3-FIFO) provided by the 'otter' library.
type MemoryCache struct {
	store otter.Cache[string, *ruleengine.FeatureFlag]
}

// NewMemoryCache initializes the in-memory cache with strict limits.
// capacity: Max number of items (Hard Cap to prevent OOM).
// ttl: Time-To-Live for items (Safety net for eventual consistency).
func NewMemoryCache(capacity int, ttl time.Duration) (*MemoryCache, error) {
	// We use the Builder pattern to construct the cache safely.
	builder, err := otter.NewBuilder[string, *ruleengine.FeatureFlag](capacity)
	if err != nil {
		return nil, err
	}

	// We must enable .CollectStats() to allow the async collector
	// to retrieve eviction/ratio data. Otter disables this by default for raw speed.
	cache, err := builder.
		WithTTL(ttl).
		CollectStats().
		Build()

	if err != nil {
		return nil, err
	}

	return &MemoryCache{store: cache}, nil
}

// Get retrieves a flag from memory.
// Returns the flag and a boolean indicating if it was found.
// This operation is virtually lock-free and extremely fast.
//
// INSTRUMENTATION:
// It increments prometheus counters for Cache Hits and Misses.
func (c *MemoryCache) Get(key string) (*ruleengine.FeatureFlag, bool) {
	val, found := c.store.Get(key)

	if found {
		observability.DataPlaneCacheHits.Inc()
	} else {
		observability.DataPlaneCacheMisses.Inc()
	}

	return val, found
}

// Set adds or updates a flag in memory.
// The TTL configured in NewMemoryCache is applied automatically.
func (c *MemoryCache) Set(key string, flag *ruleengine.FeatureFlag) {
	c.store.Set(key, flag)
}

// Del removes a flag from memory.
// Used primarily by the Pub/Sub listener when an invalidation event is received.
func (c *MemoryCache) Del(key string) {
	c.store.Delete(key)
}

// Close gracefully shuts down the cache and its background cleanup goroutines.
func (c *MemoryCache) Close() {
	c.store.Close()
}

// RunMetricsMonitor runs a blocking background loop to update async cache metrics.
// It is designed to be run in a separate goroutine controlled by the caller.
//
// Usage:
// cache := NewMemoryCache(...)
// go cache.RunMetricsMonitor(ctx, 5*time.Second)
func (c *MemoryCache) RunMetricsMonitor(ctx context.Context, interval time.Duration) {
	log := logger.FromContext(ctx).With(slog.String("component", "cache_monitor"))
	log.Info("starting cache monitor", slog.Duration("interval", interval))

	timer := time.NewTimer(interval)
	defer timer.Stop()

	// State variables to calculate deltas (Otter returns cumulative totals)
	var lastEvicted int64
	var lastRejected int64

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			// Snapshot current stats
			stats := c.store.Stats()

			// 1. Gauge: Item Count
			// Size() returns the item count (int), cast to float64 for Prometheus.
			observability.DataPlaneCacheUsage.Set(float64(c.store.Size()))

			// 2. Counter: Evictions (Delta Calculation)
			// We calculate the difference since the last tick to feed the Counter.Add().
			currentEvicted := stats.EvictedCount()
			if delta := currentEvicted - lastEvicted; delta > 0 {
				observability.DataPlaneCacheEvictions.Add(float64(delta))
				lastEvicted = currentEvicted
			}

			// 3. Counter: Dropped/Rejected (Delta Calculation)
			// Items rejected due to full buffers or policy limits.
			currentRejected := stats.RejectedSets()
			if delta := currentRejected - lastRejected; delta > 0 {
				observability.DataPlaneCacheDropped.Add(float64(delta))
				lastRejected = currentRejected
			}

			timer.Reset(interval)
		}
	}
}
