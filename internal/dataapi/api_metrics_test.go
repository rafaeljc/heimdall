//go:build integration

package dataapi_test

import (
	"context"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/rafaeljc/heimdall/internal/cache"
	"github.com/rafaeljc/heimdall/internal/config"
	"github.com/rafaeljc/heimdall/internal/dataapi"
	"github.com/rafaeljc/heimdall/internal/ruleengine"
	"github.com/rafaeljc/heimdall/internal/testsupport"
	pb "github.com/rafaeljc/heimdall/proto/heimdall/v1"
)

// setupEnvWithMetrics mirrors setupEnv but INJECTS the Observability Interceptor.
func setupEnvWithMetrics(t *testing.T) (pb.DataPlaneClient, cache.Service, func()) {
	t.Helper()

	// 1. Silent Logger
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// 2. Infrastructure: Redis
	ctx := context.Background()
	redisContainer, err := testsupport.StartRedisContainer(ctx)
	require.NoError(t, err)

	// 3. Service Init
	endpoint, _ := redisContainer.Container.PortEndpoint(ctx, "6379/tcp", "")
	host, port, _ := strings.Cut(endpoint, ":")
	redisConfig := &config.RedisConfig{
		Host: host, Port: port, PingMaxRetries: 5, PingBackoff: 2 * time.Second,
	}

	redisClient, err := cache.NewRedisClient(ctx, redisConfig)
	require.NoError(t, err)
	l2 := cache.NewRedisCache(redisClient)
	engine := ruleengine.New(log)
	dataConfig := &config.DataPlaneConfig{L1CacheCapacity: 1000, L1CacheTTL: 30 * time.Second}

	l1, err := cache.NewMemoryCache(dataConfig.L1CacheCapacity, dataConfig.L1CacheTTL)
	require.NoError(t, err)

	api, err := dataapi.NewAPI(log, l1, l2, engine)
	require.NoError(t, err)

	// 4. gRPC Server WITH INTERCEPTORS
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			dataapi.RequestLoggerInterceptor(),
			dataapi.ObservabilityInterceptor(), // Critical for this test suite
		),
	)
	pb.RegisterDataPlaneServer(s, api)

	go func() { _ = s.Serve(lis) }()

	// 5. Client
	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	client := pb.NewDataPlaneClient(conn)

	cleanup := func() {
		_ = conn.Close()
		s.Stop()
		api.Close()
		_ = l2.Close()
		_ = redisContainer.Terminate(context.Background())
	}

	return client, l2, cleanup
}

func TestApiMetrics_Integration(t *testing.T) {
	client, l2, cleanup := setupEnvWithMetrics(t)
	defer cleanup()

	ctx := context.Background()

	// A. Request Metrics (Success/Error/Latency)
	t.Run("records grpc request metrics", func(t *testing.T) {
		req := &pb.EvaluateRequest{FlagKey: "missing-flag"}
		labels := map[string]string{
			"method": "/heimdall.v1.DataPlane/Evaluate",
			"code":   "NotFound",
		}

		testsupport.AssertMetricDelta(t, "heimdall_data_plane_grpc_requests_total", labels, 1, func() {
			_, err := client.Evaluate(ctx, req)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.NotFound, st.Code())
		})

		testsupport.AssertHistogramRecorded(t, "heimdall_data_plane_grpc_handling_seconds", labels)
	})

	// B. Invalidation Metrics (Pub/Sub)
	t.Run("records invalidation metrics", func(t *testing.T) {
		testsupport.AssertMetricDeltaAsync(t, "heimdall_data_plane_l1_invalidations_total", nil, 1, func() {
			// Publish invalidation event via L2
			err := l2.BroadcastUpdate(ctx, "target-flag")
			require.NoError(t, err)
		})
	})

	// C. System Failure Metrics
	t.Run("records metrics for System Failure", func(t *testing.T) {
		l2.Close() // Sabotage Redis

		req := &pb.EvaluateRequest{FlagKey: "any"}
		labels := map[string]string{
			"code": "Internal",
		}

		testsupport.AssertMetricDelta(t, "heimdall_data_plane_grpc_requests_total", labels, 1, func() {
			_, err := client.Evaluate(ctx, req)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.Internal, st.Code())
		})
	})
}
