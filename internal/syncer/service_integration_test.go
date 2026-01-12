//go:build integration

package syncer_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rafaeljc/heimdall/internal/config"
	"github.com/rafaeljc/heimdall/internal/ruleengine"
	"github.com/rafaeljc/heimdall/internal/store"
	"github.com/rafaeljc/heimdall/internal/syncer"
	"github.com/rafaeljc/heimdall/internal/testsupport"
)

func TestSyncerService_Integration(t *testing.T) {
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

	// Seed Data
	prefix := fmt.Sprintf("boot-%d", time.Now().UnixNano())
	for i := range 20 {
		f := &store.Flag{
			Key:     fmt.Sprintf("%s-%d", prefix, i),
			Name:    "Bootstrap Flag",
			Enabled: true,
			Version: 1,
			Rules:   []ruleengine.Rule{},
		}
		require.NoError(t, repo.CreateFlag(ctx, f))
	}

	// 2. Helper to Reset Environment & Start Syncer
	// Ensures each test starts with a clean slate
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
	// SCENARIO 1: Initial Hydration
	// -------------------------------------------------------------------------
	t.Run("Should hydrate Redis on startup", func(t *testing.T) {
		// Arrange
		// Flags already exist on Postgres from setup

		// Act
		doneCh, _, cancel := startCleanSyncer(t)
		defer func() {
			cancel()
			<-doneCh
		}()

		// Assert: Marker
		require.Eventually(t, func() bool {
			val, _ := verifierClient.Get(ctx, "heimdall:sys:hydrated").Result()
			return val == "1"
		}, 5*time.Second, 100*time.Millisecond)

		// Assert: Keys
		keys, err := verifierClient.Keys(ctx, fmt.Sprintf("heimdall:flag:%s-*", prefix)).Result()
		require.NoError(t, err)
		assert.Len(t, keys, 20)
	})

	// -------------------------------------------------------------------------
	// SCENARIO 2: Event-Driven Update
	// -------------------------------------------------------------------------
	t.Run("Should process update events from queue", func(t *testing.T) {
		uniqueKey := fmt.Sprintf("rt-flag-%d", time.Now().UnixNano())
		newFlag := &store.Flag{
			Key:     uniqueKey,
			Name:    "RT Feature",
			Enabled: false,
			Version: 1,
		}
		require.NoError(t, repo.CreateFlag(ctx, newFlag))

		doneCh, _, cancel := startCleanSyncer(t)
		defer func() {
			cancel()
			<-doneCh
		}()

		// Push Event
		err := verifierClient.LPush(ctx, "heimdall:queue:updates", uniqueKey).Err()
		require.NoError(t, err)

		// Assert Update
		redisKey := fmt.Sprintf("heimdall:flag:%s", uniqueKey)
		require.Eventually(t, func() bool {
			exists, _ := verifierClient.Exists(ctx, redisKey).Result()
			return exists > 0
		}, 5*time.Second, 50*time.Millisecond)
	})

	// -------------------------------------------------------------------------
	// SCENARIO 3: Watchdog Re-Hydration
	// -------------------------------------------------------------------------
	t.Run("Watchdog should re-hydrate if marker is lost", func(t *testing.T) {
		// Arrange: Start the service normally
		// Flags already exist on Postgres from setup
		doneCh, _, cancel := startCleanSyncer(t)
		defer func() {
			cancel()
			<-doneCh
		}()

		// Wait for Steady State
		require.Eventually(t, func() bool {
			val, _ := verifierClient.Get(ctx, "heimdall:sys:hydrated").Result()
			return val == "1"
		}, 2*time.Second, 50*time.Millisecond, "Failed to reach initial state")

		// Act: Simulate Redis Crash/Data Loss
		require.NoError(t, verifierClient.FlushAll(ctx).Err())
		exists, _ := verifierClient.Exists(ctx, "heimdall:sys:hydrated").Result()
		require.Equal(t, int64(0), exists, "Key should be absent after flush")

		// Assert: Self-Healing
		// Watchdog checks every 500ms, so give it a few cycles
		require.Eventually(t, func() bool {
			val, _ := verifierClient.Get(ctx, "heimdall:sys:hydrated").Result()
			return val == "1"
		}, 3*time.Second, 100*time.Millisecond, "Watchdog failed to detect data loss and re-hydrate")
	})
}
