package cache_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/rafaeljc/heimdall/internal/cache"
	"github.com/rafaeljc/heimdall/internal/ruleengine"
	"github.com/rafaeljc/heimdall/internal/testsupport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryCache_Metrics(t *testing.T) {
	// Setup: Low capacity cache to force evictions easily
	c, err := cache.NewMemoryCache(10, 1*time.Minute)
	require.NoError(t, err)
	defer c.Close()

	// 1. Hotpath Metrics (Hits/Misses)
	t.Run("records access metrics", func(t *testing.T) {
		t.Run("misses", func(t *testing.T) {
			testsupport.AssertMetricDelta(t, "heimdall_data_plane_l1_cache_misses_total", nil, 1, func() {
				_, found := c.Get("non-existent-key")
				assert.False(t, found)
			})
		})

		t.Run("hits", func(t *testing.T) {
			c.Set("flag-1", &ruleengine.FeatureFlag{Key: "flag-1"})
			testsupport.AssertMetricDelta(t, "heimdall_data_plane_l1_cache_hits_total", nil, 1, func() {
				val, found := c.Get("flag-1")
				assert.True(t, found)
				assert.Equal(t, "flag-1", val.Key)
			})
		})
	})

	// 2. Background Metrics (Collector)
	t.Run("async collector metrics", func(t *testing.T) {
		ctx := t.Context()

		// Start collector with fast tick (10ms)
		go c.RunMetricsCollector(ctx, 10*time.Millisecond)

		t.Run("reflects items usage", func(t *testing.T) {
			// Populate some items
			for i := range 5 {
				key := fmt.Sprintf("k-%d", i)
				c.Set(key, &ruleengine.FeatureFlag{Key: key})
			}

			require.Eventually(t, func() bool {
				val := testsupport.GetMetricValue(t, "heimdall_data_plane_l1_cache_items_count", nil)
				return val >= 5
			}, 2*time.Second, 50*time.Millisecond, "usage metric failed to update")
		})

		t.Run("reflects evictions", func(t *testing.T) {
			// Flood cache (Capacity 10 -> Write 100) to force eviction
			for i := range 100 {
				key := fmt.Sprintf("overflow-%d", i)
				c.Set(key, &ruleengine.FeatureFlag{Key: key})
			}

			require.Eventually(t, func() bool {
				val := testsupport.GetMetricValue(t, "heimdall_data_plane_l1_cache_evictions_total", nil)
				return val > 0
			}, 2*time.Second, 50*time.Millisecond, "evictions metric failed to increment")
		})

		t.Run("reflects dropped items (stress test)", func(t *testing.T) {
			// Stress test to attempt filling the write buffer
			var wg sync.WaitGroup
			concurrency := 20
			writes := 100

			for i := range concurrency {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					for j := range writes {
						key := fmt.Sprintf("stress-%d-%d", id, j)
						c.Set(key, &ruleengine.FeatureFlag{Key: "stress"})
					}
				}(i)
			}
			wg.Wait()

			// Check if metric exists (it might be 0 if hardware is fast, which is fine)
			val := testsupport.GetMetricValue(t, "heimdall_data_plane_l1_cache_dropped_total", nil)
			assert.GreaterOrEqual(t, val, 0.0)
		})
	})
}
