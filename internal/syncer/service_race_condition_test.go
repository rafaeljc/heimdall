//go:build integration

package syncer_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rafaeljc/heimdall/internal/cache"
	"github.com/rafaeljc/heimdall/internal/config"
	"github.com/rafaeljc/heimdall/internal/ruleengine"
	"github.com/rafaeljc/heimdall/internal/store"
	"github.com/rafaeljc/heimdall/internal/syncer"
	"github.com/rafaeljc/heimdall/internal/testsupport"
)

// monitorVersionHistory continuously polls Redis for a flag's version and records all observed versions.
// Returns a channel that will receive the version history when the context is cancelled.
func monitorVersionHistory(ctx context.Context, client *redis.Client, flagKey string, pollInterval time.Duration) <-chan []int64 {
	resultCh := make(chan []int64, 1)

	go func() {
		var versions []int64
		var lastVersion int64
		redisKey := fmt.Sprintf("heimdall:flag:%s", flagKey)
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				resultCh <- versions
				return
			case <-ticker.C:
				val, err := client.Get(ctx, redisKey).Result()
				if err != nil {
					continue
				}

				// Extract version from "version|json" format
				pipePos := strings.IndexByte(val, '|')
				if pipePos <= 0 {
					continue
				}

				versionStr := val[:pipePos]
				version, err := strconv.ParseInt(versionStr, 10, 64)
				if err != nil {
					continue
				}

				// Only record when version changes
				if version != lastVersion {
					versions = append(versions, version)
					lastVersion = version
				}
			}
		}
	}()

	return resultCh
}

// assertMonotonicity verifies that a sequence of versions is monotonically non-decreasing.
func assertMonotonicity(t *testing.T, versions []int64, flagKey string) {
	t.Helper()
	require.NotEmpty(t, versions, "No versions observed for flag %s", flagKey)

	for i := 1; i < len(versions); i++ {
		if versions[i] < versions[i-1] {
			t.Errorf("Version regression detected for flag %s: %d → %d at position %d\nFull history: %v",
				flagKey, versions[i-1], versions[i], i, versions)
			return
		}
	}

	t.Logf("Flag %s: Monotonicity verified. Observed %d version changes: %v", flagKey, len(versions), versions)
}

func TestSyncerRaceCondition_Integration(t *testing.T) {
	// 1. Infrastructure Setup
	ctx := context.Background()

	pgCtr, err := testsupport.StartPostgresContainer(ctx, "../../migrations")
	require.NoError(t, err)
	defer pgCtr.Terminate(ctx)

	redisCtr, err := testsupport.StartRedisContainer(ctx)
	require.NoError(t, err)
	defer redisCtr.Terminate(ctx)

	// Postgres Connection
	pgPool, err := pgxpool.New(ctx, pgCtr.ConnectionString)
	require.NoError(t, err)
	defer pgPool.Close()

	repo := store.NewPostgresStore(pgPool)

	// Redis Verifier Client
	endpoint, _ := redisCtr.Container.PortEndpoint(ctx, "6379/tcp", "")
	verifierClient := redis.NewClient(&redis.Options{Addr: endpoint})
	defer verifierClient.Close()

	// 2. Helper to Reset Environment & Start Syncer
	startCleanSyncer := func(t *testing.T) (chan error, *syncer.Service, context.CancelFunc) {
		// Clean Redis before test
		require.NoError(t, verifierClient.FlushAll(ctx).Err())

		// Config (Aggressive)
		cfg := config.SyncerConfig{
			PopTimeout:             50 * time.Millisecond,
			HydrationCheckInterval: 500 * time.Millisecond,
			MaxRetries:             1,
			BaseRetryDelay:         5 * time.Millisecond,
			HydrationConcurrency:   10,
		}

		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		svc := syncer.New(logger, cfg, repo, redisCtr.Client)

		// Start Async
		syncCtx, cancel := context.WithCancel(ctx)
		doneCh := make(chan error, 1)

		go func() {
			doneCh <- svc.Run(syncCtx)
		}()

		return doneCh, svc, cancel
	}

	// -------------------------------------------------------------------------
	// SCENARIO 1: Should reject stale update when newer version exists in Redis
	// -------------------------------------------------------------------------
	t.Run("Should reject stale update when newer version exists in Redis", func(t *testing.T) {
		flagKey := fmt.Sprintf("race-stale-%d", time.Now().UnixNano())

		// Create initial flag
		flag := &store.Flag{
			Key:     flagKey,
			Name:    "Race Test Flag",
			Enabled: true,
			Version: 1,
			Rules:   []ruleengine.Rule{},
		}
		require.NoError(t, repo.CreateFlag(ctx, flag))

		doneCh, _, cancel := startCleanSyncer(t)
		defer func() {
			cancel()
			<-doneCh
		}()

		// Wait for hydration
		require.Eventually(t, func() bool {
			val, _ := verifierClient.Get(ctx, "heimdall:sys:hydrated").Result()
			return val == "1"
		}, 5*time.Second, 100*time.Millisecond)

		// Update flag to version 5 in database
		for i := range 4 {
			enabled := i%2 == 0
			params := &store.UpdateFlagParams{
				Key:     flagKey,
				Version: flag.Version,
				Enabled: &enabled,
			}
			updatedFlag, err := repo.UpdateFlag(ctx, params)
			require.NoError(t, err)
			flag = updatedFlag
		}
		require.Equal(t, int64(5), flag.Version)

		// Push version 5 update and wait for it to be processed
		err := verifierClient.LPush(ctx, cache.UpdateQueueKey, cache.EncodeQueueMessage(flagKey, 5)).Err()
		require.NoError(t, err)

		redisKey := fmt.Sprintf("heimdall:flag:%s", flagKey)
		require.Eventually(t, func() bool {
			val, err := verifierClient.Get(ctx, redisKey).Result()
			if err != nil {
				return false
			}
			decoded := cache.DecodeFlagForTest(val)
			return strings.Contains(decoded, `"version":5`)
		}, 3*time.Second, 50*time.Millisecond)

		// Now push stale updates (versions 2, 3, 4)
		for v := int64(2); v <= 4; v++ {
			err := verifierClient.LPush(ctx, cache.UpdateQueueKey, cache.EncodeQueueMessage(flagKey, v)).Err()
			require.NoError(t, err)
		}

		// Wait a bit for processing
		time.Sleep(300 * time.Millisecond)

		// Assert: Version should still be 5
		val, err := verifierClient.Get(ctx, redisKey).Result()
		require.NoError(t, err)
		require.NotEmpty(t, val)

		// Decode and verify version
		decoded := cache.DecodeFlagForTest(val)
		assert.Contains(t, decoded, `"version":5`, "Version should remain 5, stale updates should be rejected")
	})

	// -------------------------------------------------------------------------
	// SCENARIO 2: Should accept newer update when older version exists in Redis
	// -------------------------------------------------------------------------
	t.Run("Should accept newer update when older version exists in Redis", func(t *testing.T) {
		flagKey := fmt.Sprintf("race-newer-%d", time.Now().UnixNano())

		// Create initial flag at version 1
		flag := &store.Flag{
			Key:     flagKey,
			Name:    "Race Newer Test",
			Enabled: false,
			Version: 1,
			Rules:   []ruleengine.Rule{},
		}
		require.NoError(t, repo.CreateFlag(ctx, flag))

		doneCh, _, cancel := startCleanSyncer(t)
		defer func() {
			cancel()
			<-doneCh
		}()

		// Wait for hydration
		require.Eventually(t, func() bool {
			val, _ := verifierClient.Get(ctx, "heimdall:sys:hydrated").Result()
			return val == "1"
		}, 5*time.Second, 100*time.Millisecond)

		redisKey := fmt.Sprintf("heimdall:flag:%s", flagKey)

		// Verify version 1 is in Redis
		require.Eventually(t, func() bool {
			val, err := verifierClient.Get(ctx, redisKey).Result()
			if err != nil || len(val) == 0 {
				return false
			}
			decoded := cache.DecodeFlagForTest(val)
			return strings.Contains(decoded, `"version":1`)
		}, 3*time.Second, 50*time.Millisecond)

		// Update flag to version 3 in database
		for range 2 {
			enabled := true
			params := &store.UpdateFlagParams{
				Key:     flagKey,
				Version: flag.Version,
				Enabled: &enabled,
			}
			updatedFlag, err := repo.UpdateFlag(ctx, params)
			require.NoError(t, err)
			flag = updatedFlag
		}
		require.Equal(t, int64(3), flag.Version)

		// Push version 3 update
		err := verifierClient.LPush(ctx, cache.UpdateQueueKey, cache.EncodeQueueMessage(flagKey, 3)).Err()
		require.NoError(t, err)

		// Assert: Version should be updated to 3
		require.Eventually(t, func() bool {
			val, err := verifierClient.Get(ctx, redisKey).Result()
			if err != nil || len(val) == 0 {
				return false
			}
			decoded := cache.DecodeFlagForTest(val)
			return strings.Contains(decoded, `"version":3`)
		}, 3*time.Second, 50*time.Millisecond)

		// Verify the flag data is correct
		val, err := verifierClient.Get(ctx, redisKey).Result()
		require.NoError(t, err)
		jsonData := cache.DecodeFlagForTest(val)
		assert.Contains(t, jsonData, `"enabled":true`)
	})

	// -------------------------------------------------------------------------
	// SCENARIO 3: Should handle soft-delete with version tracking
	// -------------------------------------------------------------------------
	t.Run("Should handle soft-delete with version tracking", func(t *testing.T) {
		flagKey := fmt.Sprintf("race-delete-%d", time.Now().UnixNano())

		// Create flag
		flag := &store.Flag{
			Key:     flagKey,
			Name:    "Delete Test Flag",
			Enabled: true,
			Version: 1,
			Rules:   []ruleengine.Rule{},
		}
		require.NoError(t, repo.CreateFlag(ctx, flag))

		doneCh, _, cancel := startCleanSyncer(t)
		defer func() {
			cancel()
			<-doneCh
		}()

		// Wait for hydration
		require.Eventually(t, func() bool {
			val, _ := verifierClient.Get(ctx, "heimdall:sys:hydrated").Result()
			return val == "1"
		}, 5*time.Second, 100*time.Millisecond)

		redisKey := fmt.Sprintf("heimdall:flag:%s", flagKey)

		// Verify flag exists in Redis
		require.Eventually(t, func() bool {
			exists, _ := verifierClient.Exists(ctx, redisKey).Result()
			return exists > 0
		}, 3*time.Second, 50*time.Millisecond)

		// Soft-delete the flag (increments version)
		newVersion, err := repo.DeleteFlag(ctx, flagKey, flag.Version)
		require.NoError(t, err)
		require.Equal(t, int64(2), newVersion)

		// Push delete update
		err = verifierClient.LPush(ctx, cache.UpdateQueueKey, cache.EncodeQueueMessage(flagKey, newVersion)).Err()
		require.NoError(t, err)

		// Assert: Flag should now be a tombstone in Redis (IsDeleted: true)
		require.Eventually(t, func() bool {
			val, err := verifierClient.Get(ctx, redisKey).Result()
			if err != nil || len(val) == 0 {
				return false
			}
			decoded := cache.DecodeFlagForTest(val)
			return strings.Contains(decoded, `"is_deleted":true`) && strings.Contains(decoded, `"version":2`)
		}, 3*time.Second, 50*time.Millisecond)

		// Now try to push an old version (version 1) - should be rejected
		err = verifierClient.LPush(ctx, cache.UpdateQueueKey, cache.EncodeQueueMessage(flagKey, 1)).Err()
		require.NoError(t, err)

		// Wait a bit for processing
		time.Sleep(300 * time.Millisecond)

		// Assert: Tombstone should still have version 2 (not overwritten by stale version 1)
		val, err := verifierClient.Get(ctx, redisKey).Result()
		require.NoError(t, err)
		decoded := cache.DecodeFlagForTest(val)
		assert.Contains(t, decoded, `"is_deleted":true`, "Tombstone should remain")
		assert.Contains(t, decoded, `"version":2`, "Version should not downgrade from 2 to 1")
	})

	// -------------------------------------------------------------------------
	// SCENARIO 4: Should handle multiple out-of-order updates correctly
	// -------------------------------------------------------------------------
	t.Run("Should handle multiple out-of-order updates correctly", func(t *testing.T) {
		flagKey := fmt.Sprintf("race-outoforder-%d", time.Now().UnixNano())

		// Create initial flag
		flag := &store.Flag{
			Key:     flagKey,
			Name:    "Out of Order Test",
			Enabled: false,
			Version: 1,
			Rules:   []ruleengine.Rule{},
		}
		require.NoError(t, repo.CreateFlag(ctx, flag))

		doneCh, _, cancel := startCleanSyncer(t)
		defer func() {
			cancel()
			<-doneCh
		}()

		// Wait for hydration
		require.Eventually(t, func() bool {
			val, _ := verifierClient.Get(ctx, "heimdall:sys:hydrated").Result()
			return val == "1"
		}, 5*time.Second, 100*time.Millisecond)

		// Create versions 2-10 in database
		for i := range 9 {
			enabled := i%2 == 0
			params := &store.UpdateFlagParams{
				Key:     flagKey,
				Version: flag.Version,
				Enabled: &enabled,
			}
			updatedFlag, err := repo.UpdateFlag(ctx, params)
			require.NoError(t, err)
			flag = updatedFlag
		}
		require.Equal(t, int64(10), flag.Version)

		// Push updates out of order: 10, 3, 7, 2, 9, 4, 8, 5, 6
		outOfOrderVersions := []int64{10, 3, 7, 2, 9, 4, 8, 5, 6}
		for _, v := range outOfOrderVersions {
			err := verifierClient.LPush(ctx, cache.UpdateQueueKey, cache.EncodeQueueMessage(flagKey, v)).Err()
			require.NoError(t, err)
		}

		redisKey := fmt.Sprintf("heimdall:flag:%s", flagKey)

		// Assert: Final version should be 10 (highest version wins)
		require.Eventually(t, func() bool {
			val, err := verifierClient.Get(ctx, redisKey).Result()
			if err != nil {
				return false
			}
			decoded := cache.DecodeFlagForTest(val)
			return strings.Contains(decoded, `"version":10`)
		}, 3*time.Second, 50*time.Millisecond)

		// Wait a bit more to ensure all messages are processed
		time.Sleep(500 * time.Millisecond)

		// Final assertion: version should be 10
		val, err := verifierClient.Get(ctx, redisKey).Result()
		require.NoError(t, err)
		decoded := cache.DecodeFlagForTest(val)
		assert.Contains(t, decoded, `"version":10`, "Final version should be 10")
	})
}

func TestSyncerRaceCondition_ConcurrentStress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent stress test in short mode")
	}

	// 1. Infrastructure Setup
	ctx := context.Background()

	pgCtr, err := testsupport.StartPostgresContainer(ctx, "../../migrations")
	require.NoError(t, err)
	defer pgCtr.Terminate(ctx)

	redisCtr, err := testsupport.StartRedisContainer(ctx)
	require.NoError(t, err)
	defer redisCtr.Terminate(ctx)

	// Postgres Connection
	pgPool, err := pgxpool.New(ctx, pgCtr.ConnectionString)
	require.NoError(t, err)
	defer pgPool.Close()

	repo := store.NewPostgresStore(pgPool)

	// Redis Verifier Client
	endpoint, _ := redisCtr.Container.PortEndpoint(ctx, "6379/tcp", "")
	verifierClient := redis.NewClient(&redis.Options{Addr: endpoint})
	defer verifierClient.Close()

	// 2. Helper to Reset Environment & Start Syncer
	startCleanSyncer := func(t *testing.T) (chan error, *syncer.Service, context.CancelFunc) {
		// Clean Redis before test
		require.NoError(t, verifierClient.FlushAll(ctx).Err())

		// Config (Aggressive)
		cfg := config.SyncerConfig{
			PopTimeout:             50 * time.Millisecond,
			HydrationCheckInterval: 500 * time.Millisecond,
			MaxRetries:             1,
			BaseRetryDelay:         5 * time.Millisecond,
			HydrationConcurrency:   10,
		}

		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		svc := syncer.New(logger, cfg, repo, redisCtr.Client)

		// Start Async
		syncCtx, cancel := context.WithCancel(ctx)
		doneCh := make(chan error, 1)

		go func() {
			doneCh <- svc.Run(syncCtx)
		}()

		return doneCh, svc, cancel
	}

	// -------------------------------------------------------------------------
	// SCENARIO 1: Should handle concurrent updates to same flag without version regression
	// -------------------------------------------------------------------------
	t.Run("Should handle concurrent updates to same flag without version regression", func(t *testing.T) {
		flagKey := fmt.Sprintf("stress-same-%d", time.Now().UnixNano())

		// Create initial flag
		flag := &store.Flag{
			Key:     flagKey,
			Name:    "Stress Same Flag",
			Enabled: true,
			Version: 1,
			Rules:   []ruleengine.Rule{},
		}
		require.NoError(t, repo.CreateFlag(ctx, flag))

		doneCh, _, cancel := startCleanSyncer(t)
		defer func() {
			cancel()
			<-doneCh
		}()

		// Wait for hydration
		require.Eventually(t, func() bool {
			val, _ := verifierClient.Get(ctx, "heimdall:sys:hydrated").Result()
			return val == "1"
		}, 5*time.Second, 100*time.Millisecond)

		// Start version monitoring
		monitorCtx, monitorCancel := context.WithCancel(ctx)
		versionsCh := monitorVersionHistory(monitorCtx, verifierClient, flagKey, 10*time.Millisecond)

		const numGoroutines = 50
		const updatesPerGoroutine = 10

		var wg sync.WaitGroup
		var successfulUpdates atomic.Int64

		// Launch concurrent updaters
		for range numGoroutines {
			wg.Add(1)
			go func() {
				defer wg.Done()

				for j := range updatesPerGoroutine {
					// Get current version
					currentFlag, err := repo.GetFlagByKey(ctx, flagKey)
					if err != nil {
						continue
					}

					// Update flag
					enabled := j%2 == 0
					params := &store.UpdateFlagParams{
						Key:     flagKey,
						Version: currentFlag.Version,
						Enabled: &enabled,
					}
					updatedFlag, err := repo.UpdateFlag(ctx, params)
					if err != nil {
						// Optimistic lock conflict, retry
						continue
					}

					successfulUpdates.Add(1)

					// Push update to queue
					_ = verifierClient.LPush(ctx, cache.UpdateQueueKey, cache.EncodeQueueMessage(flagKey, updatedFlag.Version)).Err()

					// Small random delay
					time.Sleep(time.Millisecond * time.Duration(j%5))
				}
			}()
		}

		wg.Wait()

		// Allow time for all updates to be processed
		time.Sleep(2 * time.Second)

		// Stop monitoring and get version history
		monitorCancel()
		observedVersions := <-versionsCh

		// Assert: Version monotonicity must be maintained
		assertMonotonicity(t, observedVersions, flagKey)

		// Get final DB version
		finalFlag, err := repo.GetFlagByKey(ctx, flagKey)
		require.NoError(t, err)

		// Log statistics
		t.Logf("Successful updates: %d/%d, Final version: %d, Version changes observed: %d",
			successfulUpdates.Load(), numGoroutines*updatesPerGoroutine, finalFlag.Version, len(observedVersions))
	})

	// -------------------------------------------------------------------------
	// SCENARIO 2: Should maintain version monotonicity under high load
	// -------------------------------------------------------------------------
	t.Run("Should maintain version monotonicity under high load", func(t *testing.T) {
		const numFlags = 10
		const numWorkers = 20
		const operationsPerWorker = 25

		// Create test flags
		flagKeys := make([]string, numFlags)
		for i := range numFlags {
			flagKeys[i] = fmt.Sprintf("stress-multi-%d-%d", time.Now().UnixNano(), i)
			flag := &store.Flag{
				Key:     flagKeys[i],
				Name:    fmt.Sprintf("Multi Stress Flag %d", i),
				Enabled: true,
				Version: 1,
				Rules:   []ruleengine.Rule{},
			}
			require.NoError(t, repo.CreateFlag(ctx, flag))
		}

		doneCh, _, cancel := startCleanSyncer(t)
		defer func() {
			cancel()
			<-doneCh
		}()

		// Wait for hydration
		require.Eventually(t, func() bool {
			val, _ := verifierClient.Get(ctx, "heimdall:sys:hydrated").Result()
			return val == "1"
		}, 5*time.Second, 100*time.Millisecond)

		// Start version monitoring for all flags
		monitorCtx, monitorCancel := context.WithCancel(ctx)
		versionChannels := make([]<-chan []int64, numFlags)
		for i, flagKey := range flagKeys {
			versionChannels[i] = monitorVersionHistory(monitorCtx, verifierClient, flagKey, 10*time.Millisecond)
		}

		var successfulUpdates atomic.Int64
		var wg sync.WaitGroup

		// Launch concurrent workers
		for w := range numWorkers {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()

				for op := range operationsPerWorker {
					// Pick a flag
					flagIdx := (workerID + op) % numFlags
					flagKey := flagKeys[flagIdx]

					// Get current flag
					currentFlag, err := repo.GetFlagByKey(ctx, flagKey)
					if err != nil {
						continue
					}

					// Update flag
					enabled := op%2 == 0
					params := &store.UpdateFlagParams{
						Key:     flagKey,
						Version: currentFlag.Version,
						Enabled: &enabled,
					}
					updatedFlag, err := repo.UpdateFlag(ctx, params)
					if err != nil {
						// Optimistic lock conflict, retry
						continue
					}

					successfulUpdates.Add(1)

					// Push update to queue
					_ = verifierClient.LPush(ctx, cache.UpdateQueueKey, cache.EncodeQueueMessage(flagKey, updatedFlag.Version)).Err()

					// Small delay
					time.Sleep(time.Millisecond * time.Duration(op%3))
				}
			}(w)
		}

		wg.Wait()

		// Allow time for all updates to be processed
		time.Sleep(3 * time.Second)

		// Stop monitoring and collect version histories
		monitorCancel()
		flagVersionHistories := make([][]int64, numFlags)
		for i := range numFlags {
			flagVersionHistories[i] = <-versionChannels[i]
		}

		// Assert: Version monotonicity must be maintained for all flags
		for i, flagKey := range flagKeys {
			assertMonotonicity(t, flagVersionHistories[i], flagKey)

			// Get final DB version
			dbFlag, err := repo.GetFlagByKey(ctx, flagKey)
			require.NoError(t, err)

			// Verify progress was made
			assert.Greater(t, dbFlag.Version, int64(1), "Flag %s should have been updated", flagKey)
		}

		// Log overall statistics
		t.Logf("Total successful updates: %d/%d across %d flags",
			successfulUpdates.Load(), numWorkers*operationsPerWorker, numFlags)
	})
}
