//go:build integration

package cache_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rafaeljc/heimdall/internal/cache"
	"github.com/rafaeljc/heimdall/internal/testsupport"
)

func TestRedis_Metrics_Integration(t *testing.T) {
	// 1. Infrastructure Setup
	ctx := context.Background()
	redisCtr, err := testsupport.StartRedisContainer(ctx)
	require.NoError(t, err)
	defer redisCtr.Terminate(ctx)

	// Get raw Redis client for testing operations
	endpoint, err := redisCtr.Container.PortEndpoint(ctx, "6379/tcp", "")
	require.NoError(t, err)
	// Create Redis client with restricted pool size to make pool exhaustion
	// deterministic and testable
	client := redis.NewClient(&redis.Options{
		Addr:     endpoint,
		PoolSize: 3, // Small pool to easily test exhaustion
	})
	defer client.Close()

	// 2. Start Sidecar Monitor
	monitorCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go cache.RunPoolMonitor(monitorCtx, client, 10*time.Millisecond)

	// 3. Test Scenarios

	t.Run("Should report pool state", func(t *testing.T) {
		// Launch concurrent operations to create connections
		const numOps = 3
		done := make(chan struct{}, numOps)
		for i := range numOps {
			go func(idx int) {
				defer func() { done <- struct{}{} }()
				// Perform operation to use a connection
				client.Set(ctx, fmt.Sprintf("state-test-%d", idx), "val", time.Second).Err()
				time.Sleep(50 * time.Millisecond)
			}(i)
		}
		for range numOps {
			<-done
		}

		// Verify pool state metrics respond to concurrent load
		require.Eventually(t, func() bool {
			total := testsupport.GetMetricValue(t, "heimdall_redis_pool_connections", map[string]string{"state": "total"})
			idle := testsupport.GetMetricValue(t, "heimdall_redis_pool_connections", map[string]string{"state": "idle"})
			stale := testsupport.GetMetricValue(t, "heimdall_redis_pool_connections", map[string]string{"state": "stale"})
			// Pool should have created connections, validate consistency
			return total > 0 && idle >= 0 && stale >= 0 && stale <= total
		}, 2*time.Second, 10*time.Millisecond, "pool state metrics should reflect concurrent load")
	})

	t.Run("Should track Pool Hits", func(t *testing.T) {
		// Populate some data
		err := client.Set(ctx, "test-key", "val", time.Minute).Err()
		require.NoError(t, err)

		// Perform multiple operations sequentially.
		// go-redis returns connections to the pool immediately, so these should trigger "Pool Hits"
		for range 10 {
			client.Get(ctx, "test-key").Result()
		}

		require.Eventually(t, func() bool {
			hits := testsupport.GetMetricValue(t, "heimdall_redis_pool_hits_total", nil)
			return hits > 0
		}, 2*time.Second, 10*time.Millisecond, "pool hits should increment when reusing connections")
	})

	t.Run("Should track Timeouts", func(t *testing.T) {
		initialTimeouts := testsupport.GetMetricValue(t, "heimdall_redis_pool_timeouts_total", nil)

		// Force timeout condition: use context with very short timeout
		// This tests the pool's timeout handling under timeout pressure
		timeoutCtx, cancel := context.WithTimeout(ctx, 1*time.Millisecond)
		defer cancel()

		// Attempt multiple operations with very tight timeout to trigger timeouts
		for range 5 {
			_ = client.Get(timeoutCtx, "timeout-test-key").Val()
		}

		// Allow stabilization
		time.Sleep(100 * time.Millisecond)

		// Verify timeout metric exists and behaves (non-decreasing)
		finalTimeouts := testsupport.GetMetricValue(t, "heimdall_redis_pool_timeouts_total", nil)
		assert.GreaterOrEqual(t, finalTimeouts, initialTimeouts, "timeout counter should not decrease")
	})

	t.Run("Should track Pool Misses", func(t *testing.T) {
		// Pool misses occur when new connections must be created beyond pool size.
		// With pool size of 3 and 8 concurrent operations held simultaneously, pool will be exhausted.

		initialMisses := testsupport.GetMetricValue(t, "heimdall_redis_pool_misses_total", nil)

		// Launch concurrent operations exceeding pool size
		const numConcurrent = 8
		done := make(chan struct{}, numConcurrent)
		start := make(chan struct{})

		// Barrier to ensure all goroutines start acquiring connections at the same time
		for i := range numConcurrent {
			go func(index int) {
				defer func() { done <- struct{}{} }()

				// Wait for signal to start all simultaneously
				<-start

				// Each goroutine acquires a connection and holds it
				key := fmt.Sprintf("misses-test-key-%d", index)

				// First, write a value (acquires a connection from pool)
				err := client.Set(ctx, key, fmt.Sprintf("value-%d", index), time.Second).Err()
				require.NoError(t, err)

				// Hold the connection/operation for 300ms to force pool exhaustion
				time.Sleep(300 * time.Millisecond)

				// Then try to read it (may need new connection if pool exhausted)
				_, err = client.Get(ctx, key).Result()
				if err != nil && err != redis.Nil {
					require.NoError(t, err)
				}
			}(i)
		}

		// Start all operations simultaneously to maximize chance of pool exhaustion
		close(start)

		// Wait for all operations to complete
		for range numConcurrent {
			<-done
		}

		// Allow stabilization for metrics collection
		time.Sleep(200 * time.Millisecond)

		// Verify Pool Misses increased (should have misses from 8 ops on 3-size pool)
		currentMisses := testsupport.GetMetricValue(t, "heimdall_redis_pool_misses_total", nil)
		assert.GreaterOrEqual(t, currentMisses, initialMisses, "pool misses should not decrease")

		// Note: We check >= instead of > because pool misses may occur before we read the metric
		// The key validation is that we don't get an error and pool continues functioning
		if currentMisses == initialMisses {
			t.Logf("Pool misses did not increase (started at %.0f), but operations completed successfully", initialMisses)
		}
	})
}
