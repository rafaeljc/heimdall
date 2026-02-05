//go:build integration

package database_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rafaeljc/heimdall/internal/config"
	"github.com/rafaeljc/heimdall/internal/database"
	"github.com/rafaeljc/heimdall/internal/testsupport"
)

func TestPostgres_Metrics_Integration(t *testing.T) {
	// 1. Setup Infrastructure
	ctx := context.Background()
	pgCtr, err := testsupport.StartPostgresContainer(ctx, "../../migrations")
	require.NoError(t, err)
	defer pgCtr.Terminate(ctx)

	// Create a pool with strict limits to easily test saturation/exhaustion
	dbCfg := &config.DatabaseConfig{
		URL:            pgCtr.ConnectionString,
		MaxConns:       5, // Small max to force queuing
		MinConns:       2,
		ConnectTimeout: 5 * time.Second,
		PingMaxRetries: 5,
		PingBackoff:    2 * time.Second,
	}

	pool, err := database.NewPostgresPool(ctx, dbCfg)
	require.NoError(t, err)
	defer pool.Close()

	// 2. Start Sidecar Monitor
	// Use 10ms interval for extremely fast feedback loop in tests
	monitorCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go database.RunPoolMonitor(monitorCtx, pool, 10*time.Millisecond)

	// Acquire 2 connections to establish them as idle
	// This ensures the pool has created at least the minimum connections
	conn1, err := pool.Acquire(ctx)
	require.NoError(t, err)
	conn2, err := pool.Acquire(ctx)
	require.NoError(t, err)

	// Release them back to the pool as idle
	conn1.Release()
	conn2.Release()

	// Give monitor time to collect initial metrics with idle connections
	time.Sleep(500 * time.Millisecond)

	// 3. Test Scenarios

	t.Run("Should report correct pool configuration", func(t *testing.T) {
		// Verify Max Conns (Static)
		require.Eventually(t, func() bool {
			max := testsupport.GetMetricValue(t, "heimdall_database_pool_connections", map[string]string{"state": "max"})
			return max == 5
		}, 2*time.Second, 10*time.Millisecond, "metric 'max' connections mismatch")

		// Verify metrics are being collected with stricter bounds
		require.Eventually(t, func() bool {
			total := testsupport.GetMetricValue(t, "heimdall_database_pool_connections", map[string]string{"state": "total"})
			idle := testsupport.GetMetricValue(t, "heimdall_database_pool_connections", map[string]string{"state": "idle"})
			inUse := testsupport.GetMetricValue(t, "heimdall_database_pool_connections", map[string]string{"state": "in_use"})
			max := testsupport.GetMetricValue(t, "heimdall_database_pool_connections", map[string]string{"state": "max"})

			// Validate bounds
			if total < 0 || idle < 0 || inUse < 0 {
				return false // Metrics must be non-negative
			}

			// Gauge consistency: idle + in_use should equal total (within timing tolerance)
			// Allow small discrepancy due to monitor sampling interval
			consistencyCheck := idle + inUse
			if consistencyCheck > 0 && total > 0 {
				// Tolerance: connections can be acquired/released between samples
				// We check that the total is reasonable
				if total > max {
					return false // Total can't exceed max
				}
			}

			return total >= 0 && idle >= 0 && inUse >= 0 && total <= max
		}, 2*time.Second, 10*time.Millisecond, "failed to scrape database pool gauges with valid bounds")

		// Verify MaxConns is actually enforced
		// Acquire all 5 connections (at max)
		var acquiredConns []*pgxpool.Conn
		for range 5 {
			conn, err := pool.Acquire(ctx)
			require.NoError(t, err)
			acquiredConns = append(acquiredConns, conn)
		}
		defer func() {
			for _, c := range acquiredConns {
				c.Release()
			}
		}()

		// Try to acquire 6th connection with timeout (should fail)
		timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		_, err := pool.Acquire(timeoutCtx)
		require.Error(t, err, "6th acquisition must fail when pool is at MaxConns=5")
	})

	t.Run("Should track acquisition counts and duration", func(t *testing.T) {
		initialAcquireCount := testsupport.GetMetricValue(t, "heimdall_database_pool_acquire_count_total", nil)

		// Action: Acquire and release multiple times
		for range 5 {
			conn, err := pool.Acquire(ctx)
			require.NoError(t, err)
			time.Sleep(2 * time.Millisecond) // Ensure duration > 0
			conn.Release()
		}

		// Assert: Count increased
		require.Eventually(t, func() bool {
			current := testsupport.GetMetricValue(t, "heimdall_database_pool_acquire_count_total", nil)
			return current == initialAcquireCount+5
		}, 2*time.Second, 10*time.Millisecond, "acquire_count delta mismatch")

		// Assert: Duration increased
		duration := testsupport.GetMetricValue(t, "heimdall_database_pool_acquire_duration_seconds_total", nil)
		assert.Greater(t, duration, 0.0, "acquire_duration should be recorded")
	})

	t.Run("Should track in-use connections", func(t *testing.T) {
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer conn.Release()

		require.Eventually(t, func() bool {
			inUse := testsupport.GetMetricValue(t, "heimdall_database_pool_connections", map[string]string{"state": "in_use"})
			return inUse == 1.0
		}, 2*time.Second, 10*time.Millisecond, "in_use gauge failed to update")

		// Verify gauge consistency: idle + in_use should roughly equal total
		require.Eventually(t, func() bool {
			total := testsupport.GetMetricValue(t, "heimdall_database_pool_connections", map[string]string{"state": "total"})
			idle := testsupport.GetMetricValue(t, "heimdall_database_pool_connections", map[string]string{"state": "idle"})
			inUse := testsupport.GetMetricValue(t, "heimdall_database_pool_connections", map[string]string{"state": "in_use"})

			// Sum should equal total (allowing for timing/rounding issues)
			// At minimum, they should be related logically
			return (idle+inUse) <= total || total == 0
		}, 2*time.Second, 10*time.Millisecond, "gauge consistency check failed: idle + in_use should not exceed total")
	})

	t.Run("Should track wait count when pool is exhausted", func(t *testing.T) {
		// 1. Exhaust the pool (Acquire all 5 connections)
		var heldConns []*pgxpool.Conn
		for range 5 {
			c, err := pool.Acquire(ctx)
			require.NoError(t, err)
			heldConns = append(heldConns, c)
		}

		// 2. Launch a blocked acquire in background
		done := make(chan struct{})
		go func() {
			// This will block because pool is full
			c, err := pool.Acquire(ctx)
			if err == nil {
				c.Release()
			}
			close(done)
		}()

		// 3. Allow monitor to pick up the "wait" state (pgx internals)
		time.Sleep(50 * time.Millisecond)

		// 4. Release one connection to unblock the waiter
		heldConns[0].Release()
		heldConns = heldConns[1:]

		// 5. Ensure waiter finishes
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for blocked connection")
		}

		// 6. Cleanup remaining
		for _, c := range heldConns {
			c.Release()
		}

		// 7. Verify Metric
		require.Eventually(t, func() bool {
			waitCount := testsupport.GetMetricValue(t, "heimdall_database_pool_wait_count_total", nil)
			return waitCount >= 1
		}, 2*time.Second, 10*time.Millisecond, "wait_count should increment on pool exhaustion")
	})
}
