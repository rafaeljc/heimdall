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
	"github.com/stretchr/testify/require"

	"github.com/rafaeljc/heimdall/internal/cache"
	"github.com/rafaeljc/heimdall/internal/config"
	"github.com/rafaeljc/heimdall/internal/ruleengine"
	"github.com/rafaeljc/heimdall/internal/store"
	"github.com/rafaeljc/heimdall/internal/syncer"
	"github.com/rafaeljc/heimdall/internal/testsupport"
)

func TestSyncer_Metrics_Integration(t *testing.T) {
	// 1. Infrastructure Setup
	ctx := context.Background()

	pgCtr, err := testsupport.StartPostgresContainer(ctx, "../../migrations")
	require.NoError(t, err)
	defer pgCtr.Terminate(ctx)

	redisCtr, err := testsupport.StartRedisContainer(ctx)
	require.NoError(t, err)
	defer redisCtr.Terminate(ctx)

	pgPool, err := pgxpool.New(ctx, pgCtr.ConnectionString)
	require.NoError(t, err)
	defer pgPool.Close()

	repo := store.NewPostgresStore(pgPool)

	// Redis client for test assertions
	endpoint, _ := redisCtr.Container.PortEndpoint(ctx, "6379/tcp", "")
	verifierClient := redis.NewClient(&redis.Options{Addr: endpoint})
	defer verifierClient.Close()

	// 2. Helper to create syncer service
	createService := func(t *testing.T) *syncer.Service {
		require.NoError(t, verifierClient.FlushAll(ctx).Err())

		cfg := config.SyncerConfig{
			PopTimeout:             100 * time.Millisecond,
			HydrationCheckInterval: 1 * time.Hour,
			MaxRetries:             1,
			BaseRetryDelay:         1 * time.Millisecond,
			HydrationConcurrency:   2,
		}

		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		return syncer.New(logger, cfg, repo, redisCtr.Cache)
	}

	t.Run("Should record job processing metrics on successful event", func(t *testing.T) {
		svc := createService(t)

		// Arrange: Create flag in database (version auto-assigned)
		flag := &store.Flag{
			Key:          "metric-success-test",
			Name:         "Metric Test Flag",
			Enabled:      true,
			DefaultValue: false,
			Rules:        []ruleengine.Rule{},
		}
		err := repo.CreateFlag(ctx, flag)
		require.NoError(t, err)

		// Enqueue update message with flag's version
		message := cache.EncodeQueueMessage(flag.Key, flag.Version, time.Now().UnixMilli())
		err = verifierClient.LPush(ctx, cache.UpdateQueueKey, message).Err()
		require.NoError(t, err)

		// Assert: Metrics should be recorded during processing
		labels := map[string]string{"status": "success"}
		testsupport.AssertMetricDeltaAsync(t, "heimdall_syncer_jobs_total", labels, 1, func() {
			// Run syncer with timeout - will process hydration + queue
			syncCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			_ = svc.Run(syncCtx)
		})

		// Verify histogram was recorded for job duration
		testsupport.AssertHistogramRecorded(t, "heimdall_syncer_job_processing_duration_seconds", nil)
	})

	t.Run("Should track queue depth over time", func(t *testing.T) {
		svc := createService(t)

		// Arrange: Create flags and populate queue
		for i := range 3 {
			flag := &store.Flag{
				Key:          fmt.Sprintf("queue-test-%d", i),
				Name:         fmt.Sprintf("Queue Test %d", i),
				Enabled:      true,
				DefaultValue: false,
				Rules:        []ruleengine.Rule{},
			}
			err := repo.CreateFlag(ctx, flag)
			require.NoError(t, err)

			message := cache.EncodeQueueMessage(flag.Key, flag.Version, time.Now().UnixMilli())
			err = verifierClient.LPush(ctx, cache.UpdateQueueKey, message).Err()
			require.NoError(t, err)
		}

		// Act: Run queue monitor only (not the full syncer pipeline)
		// Monitor with 100ms interval to capture multiple readings
		monitorCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		initialDepth, err := verifierClient.LLen(ctx, cache.UpdateQueueKey).Result()
		require.NoError(t, err)
		require.Equal(t, int64(3), initialDepth, "Queue should have 3 messages initially")

		// Run queue monitor - should repeatedly record queue depth metric
		_ = svc.RunQueueMonitorOnly(monitorCtx, 100*time.Millisecond)

		// Wait for monitor to finish
		<-monitorCtx.Done()

		// Assert: Queue depth metric should have been recorded during monitoring
		queueDepthMetric := testsupport.GetMetricValue(t, "heimdall_redis_queue_depth", nil)
		require.Greater(t, queueDepthMetric, float64(0), "Queue depth metric should be recorded and > 0 (queue not consumed)")
		require.Equal(t, float64(3), queueDepthMetric, "Queue depth metric should reflect the 3 queued messages")
	})
}
