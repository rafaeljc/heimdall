//go:build integration

package health_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rafaeljc/heimdall/internal/config"
	"github.com/rafaeljc/heimdall/internal/health"
	"github.com/rafaeljc/heimdall/internal/logger"
	"github.com/rafaeljc/heimdall/internal/testsupport"
)

// TestHealthService_Integration validates the health check endpoints with real dependencies.
func TestHealthService_Integration(t *testing.T) {
	// 1. Infrastructure Setup (Arrange)
	ctx := context.Background()

	// Start Postgres
	pgContainer, err := testsupport.StartPostgresContainer(ctx, "../../migrations")
	require.NoError(t, err, "failed to start postgres container")
	defer func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate postgres container: %v", err)
		}
	}()

	// Start Redis
	redisContainer, err := testsupport.StartRedisContainer(ctx)
	require.NoError(t, err, "failed to start redis container")
	defer func() {
		if err := redisContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate redis container: %v", err)
		}
	}()

	// 2. Wiring & Configuration

	// We need a raw Redis client for the health checker, passing the connection string from the container
	redisEndpoint, err := redisContainer.Container.PortEndpoint(ctx, "6379/tcp", "")
	require.NoError(t, err)
	redisClient := redis.NewClient(&redis.Options{Addr: redisEndpoint})
	defer redisClient.Close()

	// Find a free port for the health server to listen on
	freePort, err := getFreePort()
	require.NoError(t, err, "failed to get free port")

	// Create configuration mimicking production
	cfg := &config.Config{
		App: config.AppConfig{
			Name:        "heimdall-test",
			Environment: "test",
			LogLevel:    "debug",
			LogFormat:   "text",
		},
		Health: config.HealthConfig{
			Port:          fmt.Sprintf("%d", freePort),
			LivenessPath:  "/healthz",
			ReadinessPath: "/readyz",
			Timeout:       1 * time.Second, // Fast timeout for tests
		},
	}

	log := logger.New(&cfg.App)

	// Initialize the Service with real checkers
	svc := health.NewService(
		log,
		cfg,
		health.NewPostgresChecker(pgContainer.DB),
		health.NewRedisChecker(redisClient),
	)

	// Start the server
	svc.Start()
	defer func() {
		_ = svc.Stop(ctx)
	}()

	// Wait for server to be ready
	baseURL := fmt.Sprintf("http://localhost:%d", freePort)
	require.Eventually(t, func() bool {
		_, err := http.Get(baseURL + "/healthz")
		return err == nil
	}, 2*time.Second, 100*time.Millisecond, "Health server failed to start")

	// -------------------------------------------------------------------------
	// SCENARIO 1: Happy Path (All Systems Up)
	// -------------------------------------------------------------------------
	t.Run("Should return 200 OK when all dependencies are healthy", func(t *testing.T) {
		// Act
		resp, err := http.Get(baseURL + "/readyz")
		require.NoError(t, err)
		defer resp.Body.Close()

		// Assert
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var body map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

		statusMap, ok := body["status"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "up", statusMap["database"])
		assert.Equal(t, "up", statusMap["redis"])
	})

	t.Run("Liveness should always return 200 OK", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/healthz")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// -------------------------------------------------------------------------
	// SCENARIO 2: Partial Failure (Redis Down)
	// -------------------------------------------------------------------------
	t.Run("Should return 503 when Redis is down", func(t *testing.T) {
		// Arrange: Stop Redis
		// Note: We use the raw container terminate, forcing a connection error
		err := redisContainer.Container.Stop(ctx, nil) // Stop without removing
		require.NoError(t, err)

		// Allow some time for the connection pool to realize the socket is closed
		time.Sleep(500 * time.Millisecond)

		// Act
		resp, err := http.Get(baseURL + "/readyz")
		require.NoError(t, err)
		defer resp.Body.Close()

		// Assert
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

		var body map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

		statusMap, ok := body["status"].(map[string]any)
		require.True(t, ok)

		// DB should still be UP
		assert.Equal(t, "up", statusMap["database"])

		// Redis should be DOWN
		redisStatus, _ := statusMap["redis"].(string)
		assert.Contains(t, redisStatus, "down")

		// Liveness should STILL be UP (The app process is fine)
		liveResp, _ := http.Get(baseURL + "/healthz")
		assert.Equal(t, http.StatusOK, liveResp.StatusCode)
		liveResp.Body.Close()

		// Restart Redis for subsequent tests (if any)
		// err = redisContainer.Container.Start(ctx)
		// require.NoError(t, err)
	})
}

// getFreePort asks the kernel for a free open port that is ready to use.
func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
