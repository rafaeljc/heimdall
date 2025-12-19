//go:build integration

package cache_test

import (
	"context"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rafaeljc/heimdall/internal/cache"
	"github.com/rafaeljc/heimdall/internal/testsupport"
)

// TestRedisCache_LuaScript_Integration verifies the core data integrity logic.
// It ensures the Lua script correctly handles Optimistic Locking (Versioning)
// and Self-Healing (Data Corruption Repair).
func TestRedisCache_LuaScript_Integration(t *testing.T) {
	// 1. Infrastructure Setup
	ctx := context.Background()

	redisCtr, err := testsupport.StartRedisContainer(ctx)
	require.NoError(t, err)
	defer redisCtr.Terminate(ctx)

	// System Under Test (SUT)
	appCache := redisCtr.Client

	// Spy Client (Side-channel verification)
	// Used to peek into Redis raw data or inject corruption.
	endpoint, err := redisCtr.Container.PortEndpoint(ctx, "6379/tcp", "")
	require.NoError(t, err)

	spyClient := redis.NewClient(&redis.Options{Addr: endpoint})
	defer spyClient.Close()

	// Test Data
	flagKey := "feature-flag-test"
	redisKey := "heimdall:flag:" + flagKey

	// -------------------------------------------------------------------------
	// SCENARIO 1: Fresh Insert (Result: Updated)
	// -------------------------------------------------------------------------
	t.Run("Should insert new flag and return SetResultUpdated(1)", func(t *testing.T) {
		payload := map[string]any{"enabled": true}
		version := int64(10)

		res, err := appCache.SetFlagSafely(ctx, flagKey, payload, version)
		require.NoError(t, err)
		assert.Equal(t, cache.SetResultUpdated, res)

		// Verification: Check storage format "version|json"
		val, err := spyClient.Get(ctx, redisKey).Result()
		require.NoError(t, err)
		assert.Contains(t, val, "10|", "Storage must include version prefix")
		assert.Contains(t, val, `"enabled":true`, "Storage must include JSON payload")
	})

	// -------------------------------------------------------------------------
	// SCENARIO 2: Valid Update (Result: Updated)
	// -------------------------------------------------------------------------
	t.Run("Should update when new version is higher and return SetResultUpdated(1)", func(t *testing.T) {
		payload := map[string]any{"enabled": false}
		newVersion := int64(11) // 11 > 10

		res, err := appCache.SetFlagSafely(ctx, flagKey, payload, newVersion)
		require.NoError(t, err)
		assert.Equal(t, cache.SetResultUpdated, res)

		// Verification
		val, _ := spyClient.Get(ctx, redisKey).Result()
		assert.Contains(t, val, "11|", "Version prefix should be updated to 11|")
	})

	// -------------------------------------------------------------------------
	// SCENARIO 3: Stale/Idempotent Update (Result: Skipped)
	// -------------------------------------------------------------------------
	t.Run("Should skip update when version is lower/equal and return SetResultSkipped(0)", func(t *testing.T) {
		payload := map[string]any{"enabled": true} // Old data attempting to overwrite

		// Case A: Lower Version (Stale from queue lag)
		staleVersion := int64(5)
		res, err := appCache.SetFlagSafely(ctx, flagKey, payload, staleVersion)
		require.NoError(t, err)
		assert.Equal(t, cache.SetResultSkipped, res, "Should skip lower version")

		// Case B: Same Version (Idempotency check)
		sameVersion := int64(11)
		res, err = appCache.SetFlagSafely(ctx, flagKey, payload, sameVersion)
		require.NoError(t, err)
		assert.Equal(t, cache.SetResultSkipped, res, "Should skip equal version")

		// Verification: Data in Redis must remain untouched (Version 11)
		val, _ := spyClient.Get(ctx, redisKey).Result()
		assert.Contains(t, val, "11|", "Redis value should remain at version 11")
	})

	// -------------------------------------------------------------------------
	// SCENARIO 4: Data Corruption Repair (Result: Repaired)
	// -------------------------------------------------------------------------
	t.Run("Should detect and repair corrupted data and return SetResultRepaired(2)", func(t *testing.T) {
		// Arrange: Sabotage!
		// We manually inject a value that violates the "ver|json" contract.
		// This simulates a legacy system write or manual admin error.
		corruptedKey := "corrupted-feature"
		corruptedRedisKey := "heimdall:flag:" + corruptedKey

		err := spyClient.Set(ctx, corruptedRedisKey, `{"raw":"json_without_pipe"}`, 0).Err()
		require.NoError(t, err)

		// Act: Try to write properly via the system
		payload := map[string]any{"repaired": true}
		version := int64(50)

		res, err := appCache.SetFlagSafely(ctx, corruptedKey, payload, version)
		require.NoError(t, err)

		// Assert: The Lua script should catch the missing pipe and force overwrite
		assert.Equal(t, cache.SetResultRepaired, res, "Should return Repaired(2) when pipe is missing")

		// Verification: Data should now be correct
		val, _ := spyClient.Get(ctx, corruptedRedisKey).Result()
		assert.Contains(t, val, "50|", "Data should be repaired with correct version prefix")
	})
}
