//go:build integration

package dataapi_test

import (
	"context"
	"fmt"
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

// bufSize defines the buffer size for the in-memory network via bufconn.
const bufSize = 1024 * 1024

// setupEnv initializes the complete environment required for integration testing:
// - Redis Container (Real L2 Cache)
// - Data Plane API (L1 Memory Cache + gRPC Server)
// - gRPC Client connected via memory (bufconn)
func setupEnv(t *testing.T) (pb.DataPlaneClient, cache.Service, func()) {
	t.Helper()

	// 1. Silent Logger (prevents pollution in CI, use os.Stdout for debugging)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// 2. Infrastructure: Redis Container
	// We use context.Background() because the container lifecycle must span the entire test.
	ctx := context.Background()
	redisContainer, err := testsupport.StartRedisContainer(ctx)
	require.NoError(t, err, "failed to start redis container")

	// 3. Service Initialization
	// Redis Cache (L2)
	endpoint, err := redisContainer.Container.PortEndpoint(ctx, "6379/tcp", "")
	require.NoError(t, err)

	// Parse endpoint into host:port for config
	host, port, _ := strings.Cut(endpoint, ":")
	redisConfig := &config.RedisConfig{
		Host:           host,
		Port:           port,
		PingMaxRetries: 5,
		PingBackoff:    2 * time.Second,
	}
	redisClient, err := cache.NewRedisClient(ctx, redisConfig)
	require.NoError(t, err)
	l2 := cache.NewRedisCache(redisClient)

	// Rule Engine (Stateless)
	engine := ruleengine.New(log)

	// Data Plane Config for API
	dataConfig := &config.DataPlaneConfig{
		L1CacheCapacity: 1000,
		L1CacheTTL:      30 * time.Second,
	}

	// API Server (System Under Test)
	api, err := dataapi.NewAPI(dataConfig, log, l2, engine)
	require.NoError(t, err)

	// 4. In-Memory Networking (bufconn)
	// Simulates TCP networking without opening real host ports, allowing full parallelism.
	lis := bufconn.Listen(bufSize)
	s := grpc.NewServer()
	pb.RegisterDataPlaneServer(s, api)

	// Start the gRPC server in a goroutine
	go func() {
		if err := s.Serve(lis); err != nil {
			panic(fmt.Sprintf("Server exited with error: %v", err))
		}
	}()

	// 5. gRPC Client
	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	client := pb.NewDataPlaneClient(conn)

	// 6. Teardown (Graceful Shutdown)
	cleanup := func() {
		_ = conn.Close()                                   // Close client connection
		s.Stop()                                           // Stop gRPC server
		api.Close()                                        // Close API (stops L1 watchers)
		_ = l2.Close()                                     // Close Redis connection
		_ = redisContainer.Terminate(context.Background()) // Kill container
	}

	return client, l2, cleanup
}

// seedFlag is a helper to simulate the Syncer behavior.
// It writes directly to Redis using the production Lua script logic.
func seedFlag(t *testing.T, l2 cache.Service, flag ruleengine.FeatureFlag, version int64) {
	t.Helper()
	_, err := l2.SetFlagSafely(context.Background(), flag.Key, flag, version)
	require.NoError(t, err, "failed to seed flag data into redis")
}

func TestDataPlane_Integration_Scenarios(t *testing.T) {
	// Single setup for the suite.
	// Since Data Plane is stateless (except L1), we can reuse the instance
	// as long as we use unique flag keys for each scenario.
	client, l2, cleanup := setupEnv(t)
	defer cleanup()

	ctx := context.Background()

	// Scenario 1: Happy Path
	// A standard flag with a matching rule should return TRUE.
	t.Run("Happy Path: Flag match returns true", func(t *testing.T) {
		flagKey := "feature-happy-path"

		seedFlag(t, l2, ruleengine.FeatureFlag{
			Key:          flagKey,
			Enabled:      true,
			DefaultValue: false,
			Rules: []ruleengine.Rule{
				{
					ID:    "allow-list-123",
					Type:  ruleengine.RuleTypeUserIDList,
					Value: []byte(`{"user_ids": ["123"]}`),
				},
			},
		}, 1)

		resp, err := client.Evaluate(ctx, &pb.EvaluateRequest{
			FlagKey: flagKey,
			Context: map[string]string{"user_id": "123"},
		})

		require.NoError(t, err)
		assert.True(t, resp.Value, "Should return true for matched user")
		assert.Equal(t, "RULE_MATCH", resp.Reason)
	})

	// Scenario 2: Real-time Update (Pub/Sub)
	// Changing a flag and broadcasting an update should reflect in the API (L1 invalidation).
	t.Run("Real-time Update: Value changes when flag is updated", func(t *testing.T) {
		flagKey := fmt.Sprintf("rt-toggle-%d", time.Now().UnixNano())

		// A. Initial State: TRUE
		seedFlag(t, l2, ruleengine.FeatureFlag{
			Key:          flagKey,
			Enabled:      true,
			DefaultValue: true,
			Rules:        []ruleengine.Rule{},
		}, 1)

		// Prime the L1 Cache
		resp, err := client.Evaluate(ctx, &pb.EvaluateRequest{FlagKey: flagKey})
		require.NoError(t, err)
		assert.True(t, resp.Value)

		// B. Update: Change to FALSE (Simulating Syncer update)
		seedFlag(t, l2, ruleengine.FeatureFlag{
			Key:     flagKey,
			Enabled: false,
		}, 2)

		// Broadcast invalidation event
		err = l2.BroadcastUpdate(ctx, flagKey)
		require.NoError(t, err)

		// C. Verify Consistency (Eventually)
		assert.Eventually(t, func() bool {
			resp, err := client.Evaluate(ctx, &pb.EvaluateRequest{FlagKey: flagKey})
			if err != nil {
				return false
			}
			// Should return FALSE (from Redis) with reason DISABLED
			return resp.Value == false && resp.Reason == "DISABLED"
		}, 2*time.Second, 50*time.Millisecond, "L1 cache should be invalidated and return new value")
	})

	// Scenario 3: Validation
	// Empty flag key should result in an invalid argument error.
	t.Run("Validation: Empty flag key returns error", func(t *testing.T) {
		_, err := client.Evaluate(ctx, &pb.EvaluateRequest{
			FlagKey: "",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok, "Should be a gRPC status error")
		assert.Equal(t, codes.InvalidArgument, st.Code(), "Should return INVALID_ARGUMENT")
	})

	// Scenario 4: Not Found
	// Queries for non-existent flags should return a Not Found error.
	t.Run("Not Found: Non-existent flag returns false (error)", func(t *testing.T) {
		_, err := client.Evaluate(ctx, &pb.EvaluateRequest{
			FlagKey: "ghost-flag-key",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code(), "Should return NOT_FOUND")
	})

	// Scenario 5: Default Value
	// If the flag is enabled but the user does not match any rule, it should return the DefaultValue (True).
	t.Run("Default Value: Flag existing without match returns default", func(t *testing.T) {
		flagKey := "feature-default-fallback"

		seedFlag(t, l2, ruleengine.FeatureFlag{
			Key:     flagKey,
			Enabled: true,
			// Crucial: Default is TRUE
			DefaultValue: true,
			Rules: []ruleengine.Rule{
				{
					ID:    "vip-only",
					Type:  ruleengine.RuleTypeUserIDList,
					Value: []byte(`{"user_ids": ["vip_user"]}`),
				},
			},
		}, 1)

		// User "guest" is NOT in the list
		resp, err := client.Evaluate(ctx, &pb.EvaluateRequest{
			FlagKey: flagKey,
			Context: map[string]string{"user_id": "guest"},
		})

		require.NoError(t, err)
		// Requirement: "use true as default"
		assert.True(t, resp.Value, "Should return DefaultValue (True) when no rules match")
		// Note: The 'Reason' assertion depends on implementation. Ideally "NO_MATCH" or "DEFAULT".
	})

	// Scenario 6: Disabled Flag
	// Even if the user is in the allow-list, if the flag is disabled globally, it must return False.
	t.Run("Disabled Flag: Returns false even if user matches", func(t *testing.T) {
		flagKey := "feature-kill-switch"

		seedFlag(t, l2, ruleengine.FeatureFlag{
			Key:     flagKey,
			Enabled: false, // GLOBAL KILL SWITCH
			Rules: []ruleengine.Rule{
				{
					ID:    "allow-all",
					Type:  ruleengine.RuleTypeUserIDList,
					Value: []byte(`{"user_ids": ["target_user"]}`),
				},
			},
		}, 1)

		resp, err := client.Evaluate(ctx, &pb.EvaluateRequest{
			FlagKey: flagKey,
			Context: map[string]string{"user_id": "target_user"},
		})

		require.NoError(t, err)
		assert.False(t, resp.Value, "Should return false because flag is disabled")
		assert.Equal(t, "DISABLED", resp.Reason, "Reason should reflect the disabled state")
	})
}
