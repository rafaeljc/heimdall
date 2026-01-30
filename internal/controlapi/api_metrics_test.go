//go:build integration

package controlapi_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rafaeljc/heimdall/internal/controlapi"
	"github.com/rafaeljc/heimdall/internal/store"
	"github.com/rafaeljc/heimdall/internal/testsupport"
)

// setupIntegrationEnv boots up real dependencies (Postgres + Redis) using Testcontainers.
// It returns a fully configured API instance and a cleanup function.
func setupIntegrationEnv(t *testing.T) (*controlapi.API, func()) {
	t.Helper()

	ctx := context.Background()

	// 1. Start Postgres (with migrations)
	migrationsPath, err := filepath.Abs("../../migrations")
	require.NoError(t, err)

	pgContainer, err := testsupport.StartPostgresContainer(ctx, migrationsPath)
	require.NoError(t, err)

	// 2. Start Redis
	redisContainer, err := testsupport.StartRedisContainer(ctx)
	require.NoError(t, err)

	// 3. Initialize Adapters
	repo := store.NewPostgresStore(pgContainer.DB)

	// 4. Initialize API (Auth disabled for testing metrics logic cleanly)
	// We pass an empty apiKeyHash because skipAuth=true
	api := controlapi.NewAPIWithConfig(repo, redisContainer.Cache, "", true)

	cleanup := func() {
		_ = pgContainer.Terminate(ctx)
		_ = redisContainer.Terminate(ctx)
	}

	return api, cleanup
}

func TestMetrics_Integration(t *testing.T) {
	// Setup infrastructure once (or per test if strict isolation is needed).
	// Since metrics are global (Prometheus registry), we run this serially.
	api, cleanup := setupIntegrationEnv(t)
	defer cleanup()

	// -------------------------------------------------------------------------
	// Scenario 1: Success Path (200 OK)
	// Focus: Verify standard request counting and latency recording.
	// -------------------------------------------------------------------------
	t.Run("records metrics for successful request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rr := httptest.NewRecorder()

		counterLabels := map[string]string{
			"method": "GET",
			"route":  "/health",
			"code":   "200",
		}

		histogramLabels := map[string]string{
			"method": "GET",
			"route":  "/health",
		}

		// Assert Counter Increment
		testsupport.AssertMetricDelta(t, "heimdall_control_plane_http_requests_total", counterLabels, 1, func() {
			api.Router.ServeHTTP(rr, req)
			require.Equal(t, http.StatusOK, rr.Code)
		})

		// Assert Latency Histogram Observation
		testsupport.AssertHistogramRecorded(t, "heimdall_control_plane_http_handling_seconds", histogramLabels)
	})

	// -------------------------------------------------------------------------
	// Scenario 2: Business Resource Not Found (404)
	// Focus: CARDINALITY PROTECTION.
	// Even though it's a 404, the route pattern matches "/api/v1/flags/{key}".
	// We MUST see the route pattern in the label, NOT the specific ID.
	// -------------------------------------------------------------------------
	t.Run("records metrics for business 404 (preserves route pattern)", func(t *testing.T) {
		// "missing-key-123" should NOT appear in Prometheus labels
		req := httptest.NewRequest(http.MethodGet, "/api/v1/flags/missing-key-123", nil)
		rr := httptest.NewRecorder()

		labels := map[string]string{
			"method": "GET",
			// Critical Check: The label must be the abstract route, not the raw path
			"route": "/api/v1/flags/{key}",
			"code":  "404",
		}

		testsupport.AssertMetricDelta(t, "heimdall_control_plane_http_requests_total", labels, 1, func() {
			api.Router.ServeHTTP(rr, req)
			require.Equal(t, http.StatusNotFound, rr.Code)
		})
	})

	// -------------------------------------------------------------------------
	// Scenario 3: Infrastructure/Attack 404
	// Focus: CARDINALITY PROTECTION.
	// Route does not exist in Chi. It MUST collapse to "not_found".
	// -------------------------------------------------------------------------
	t.Run("records metrics for infra 404 (collapses to not_found)", func(t *testing.T) {
		// Random path scanning
		req := httptest.NewRequest(http.MethodGet, "/admin.php", nil)
		rr := httptest.NewRecorder()

		labels := map[string]string{
			"method": "GET",
			// Critical Check: Prevents explosion of labels for attacks
			"route": "not_found",
			"code":  "404",
		}

		testsupport.AssertMetricDelta(t, "heimdall_control_plane_http_requests_total", labels, 1, func() {
			api.Router.ServeHTTP(rr, req)
			require.Equal(t, http.StatusNotFound, rr.Code)
		})
	})

	// -------------------------------------------------------------------------
	// Scenario 4: Bad Request (400)
	// Focus: Error counting.
	// -------------------------------------------------------------------------
	t.Run("records metrics for bad request", func(t *testing.T) {
		// Sending broken JSON
		brokenJSON := []byte(`{invalid-json`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/flags", bytes.NewBuffer(brokenJSON))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		labels := map[string]string{
			"method": "POST",
			"route":  "/api/v1/flags",
			"code":   "400",
		}

		testsupport.AssertMetricDelta(t, "heimdall_control_plane_http_requests_total", labels, 1, func() {
			api.Router.ServeHTTP(rr, req)
			// Ensure the API actually rejected it
			require.Equal(t, http.StatusBadRequest, rr.Code)
		})
	})
}
