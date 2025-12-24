package cache

import (
	"time"

	"github.com/maypok86/otter"
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
	// otter.MustBuilder panics on error, so we use Builder to be safe if desired,
	// but here we wrap it to return the error.
	builder := otter.MustBuilder[string, *ruleengine.FeatureFlag](capacity).
		WithTTL(ttl)

	cache, err := builder.Build()
	if err != nil {
		return nil, err
	}

	return &MemoryCache{store: cache}, nil
}

// Get retrieves a flag from memory.
// Returns the flag and a boolean indicating if it was found.
// This operation is virtually lock-free and extremely fast.
func (c *MemoryCache) Get(key string) (*ruleengine.FeatureFlag, bool) {
	return c.store.Get(key)
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
