//go:build integration

package observability_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rafaeljc/heimdall/internal/cache"
	"github.com/rafaeljc/heimdall/internal/config"
	"github.com/rafaeljc/heimdall/internal/database"
	"github.com/rafaeljc/heimdall/internal/logger"
	"github.com/rafaeljc/heimdall/internal/observability"
	"github.com/rafaeljc/heimdall/internal/testsupport"
)

func TestObservabilityServer_Integration(t *testing.T) {
	// 1. Arrange: Infrastructure Setup
	ctx := context.Background()

	// Start Postgres Container
	// We point to the migrations folder relative to this test file.
	pgContainer, err := testsupport.StartPostgresContainer(ctx, "../../migrations")
	require.NoError(t, err)
	defer pgContainer.Terminate(ctx)

	// Start Redis Container
	redisContainer, err := testsupport.StartRedisContainer(ctx)
	require.NoError(t, err)
	defer redisContainer.Terminate(ctx)

	// 2. Arrange: Connections and Checkers

	// Postgres Connection & Checker
	dbPool, err := pgxpool.New(ctx, pgContainer.ConnectionString)
	require.NoError(t, err)
	defer dbPool.Close()

	pgChecker := database.NewHealthChecker(dbPool)

	// Redis Connection & Checker
	redisEndpoint, _ := redisContainer.Container.PortEndpoint(ctx, "6379/tcp", "")
	redisClient := redis.NewClient(&redis.Options{Addr: redisEndpoint})
	defer redisClient.Close()

	redisChecker := cache.NewHealthChecker(redisClient)

	// 3. Arrange: Server Configuration
	freePort, _ := getFreePort()

	// QA TRICK: We use non-standard paths to ensure the server is respecting the configuration
	// instead of using hardcoded defaults.
	livenessPath := "/alive"
	readinessPath := "/check-deps"
	metricsPath := "/telemetry"

	appCfg := &config.AppConfig{
		Name:        "heimdall-test",
		Version:     "v0.0.0-test",
		Environment: "development",
		LogLevel:    "debug",
		LogFormat:   "text",
	}

	obsCfg := &config.ObservabilityConfig{
		Port:          fmt.Sprintf("%d", freePort),
		Timeout:       1 * time.Second,
		LivenessPath:  livenessPath,
		ReadinessPath: readinessPath,
		MetricsPath:   metricsPath,
	}

	log := logger.New(appCfg)

	// Inject dependencies into the server
	server := observability.NewServer(log, obsCfg, pgChecker, redisChecker)

	// 4. Act: Start Server
	server.Start()
	defer func() { _ = server.Shutdown(ctx) }()

	baseURL := fmt.Sprintf("http://localhost:%d", freePort)

	// Wait for server to be ready using the CUSTOM liveness path.
	// This confirms the server started and the config was applied correctly.
	require.Eventually(t, func() bool {
		resp, err := http.Get(baseURL + livenessPath)
		if err == nil {
			resp.Body.Close()
			return resp.StatusCode == http.StatusOK
		}
		return false
	}, 5*time.Second, 100*time.Millisecond, "Server failed to start")

	// 5. Assert: Test Endpoints

	t.Run("Liveness should return 200 OK on custom path", func(t *testing.T) {
		resp, err := http.Get(baseURL + livenessPath)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		body, _ := io.ReadAll(resp.Body)
		assert.Equal(t, "ok", string(body))
	})

	t.Run("Metrics should be exposed on custom path", func(t *testing.T) {
		resp, err := http.Get(baseURL + metricsPath)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		body, _ := io.ReadAll(resp.Body)

		// Verify if it contains standard Prometheus metrics
		bodyStr := string(body)
		assert.Contains(t, bodyStr, "go_goroutines")
		assert.Contains(t, bodyStr, "heimdall_")
	})

	t.Run("Readiness should return 200 OK on custom path when deps are healthy", func(t *testing.T) {
		resp, err := http.Get(baseURL + readinessPath)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)

		// Assert dependencies status
		statusMap := body["status"].(map[string]any)
		assert.Equal(t, "up", statusMap["postgres"])
		assert.Equal(t, "up", statusMap["redis"])
	})

	t.Run("Readiness should fail (503) when Redis is down", func(t *testing.T) {
		// Simulate failure by stopping the Redis container
		_ = redisContainer.Container.Stop(ctx, nil)

		// Allow some time for the client to detect the connection loss
		time.Sleep(200 * time.Millisecond)

		resp, err := http.Get(baseURL + readinessPath)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		statusMap := body["status"].(map[string]any)

		// Verify that Redis is marked as down
		redisStatus := statusMap["redis"].(string)
		assert.Contains(t, redisStatus, "down")
	})
}

// getFreePort asks the kernel for a free TCP port.
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
