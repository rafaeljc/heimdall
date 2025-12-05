//go:build integration

package controlapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rafaeljc/heimdall/internal/controlapi"
	"github.com/rafaeljc/heimdall/internal/store"
	"github.com/rafaeljc/heimdall/internal/testsupport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestControlPlaneAPI_Integration validates the full HTTP request lifecycle.
// It ensures that Routing, Middleware, JSON Serialization, Validation, and DB Persistence
// work together as defined in the OpenAPI contract.
func TestControlPlaneAPI_Integration(t *testing.T) {
	// 1. Infrastructure Setup (Arrange)
	ctx := context.Background()

	// Connect to ephemeral PostgreSQL container
	pgContainer, err := testsupport.StartPostgresContainer(ctx, "../../migrations")
	require.NoError(t, err, "failed to start postgres container")

	// Ensure resource cleanup happens after tests finish
	defer func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}()

	// 2. Application Wiring
	// Initialize real dependencies (Repository -> DB)
	repo := store.NewPostgresStore(pgContainer.DB)
	// Initialize API with dependency injection
	api := controlapi.NewAPI(repo)

	// -------------------------------------------------------------------------
	// SCENARIO 1: POST /flags (Creation & Validation)
	// -------------------------------------------------------------------------

	t.Run("POST /flags - Happy Path (Full Payload)", func(t *testing.T) {
		// Arrange: Use a unique key to ensure isolation from other tests
		key := fmt.Sprintf("feature-full-%d", time.Now().UnixNano())

		input := controlapi.CreateFlagRequest{
			Key:          key,
			Name:         "Full Feature",
			Description:  "Description",
			Enabled:      true,
			DefaultValue: true,
		}
		body, _ := json.Marshal(input)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/flags", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		// Act
		api.Router.ServeHTTP(rr, req)

		// Assert: HTTP Contract
		require.Equal(t, http.StatusCreated, rr.Code)

		// Assert: Data Contract
		var resp controlapi.Flag
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))

		// Validate input mapping
		assert.Equal(t, input.Key, resp.Key)
		assert.Equal(t, input.Name, resp.Name)
		assert.Equal(t, input.Description, resp.Description)
		assert.Equal(t, input.Enabled, resp.Enabled)
		assert.Equal(t, input.DefaultValue, resp.DefaultValue)

		// Validate server-generated fields
		assert.NotZero(t, resp.ID, "Server must generate ID")
		assert.False(t, resp.CreatedAt.IsZero(), "Server must generate CreatedAt")
		assert.False(t, resp.UpdatedAt.IsZero(), "Server must generate UpdatedAt")
	})

	t.Run("POST /flags - Happy Path (Defaults Check)", func(t *testing.T) {
		// Arrange: Send payload WITHOUT 'enabled' and 'default_value'
		key := fmt.Sprintf("feature-defaults-%d", time.Now().UnixNano())
		payload := map[string]interface{}{
			"key":  key,
			"name": "Defaults Feature",
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/flags", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		// Act
		api.Router.ServeHTTP(rr, req)

		// Assert
		require.Equal(t, http.StatusCreated, rr.Code)
		var resp controlapi.Flag
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))

		// Validate "Secure by Default" behavior
		assert.False(t, resp.Enabled, "should default to disabled (false)")
		assert.False(t, resp.DefaultValue, "should default to false")
	})

	t.Run("POST /flags - Validation & Type Safety", func(t *testing.T) {
		longString := strings.Repeat("a", 256)

		tests := []struct {
			name           string
			payload        map[string]interface{}
			expectedStatus int
			expectedCode   string
		}{
			// --- KEY VALIDATION ---
			{
				name:           "Key Missing",
				payload:        map[string]interface{}{"name": "No Key"},
				expectedStatus: http.StatusBadRequest,
				expectedCode:   "ERR_INVALID_INPUT",
			},
			{
				name:           "Key Too Short",
				payload:        map[string]interface{}{"key": "ab", "name": "Short"},
				expectedStatus: http.StatusBadRequest,
				expectedCode:   "ERR_INVALID_INPUT",
			},
			{
				name:           "Key Too Long",
				payload:        map[string]interface{}{"key": longString, "name": "Long"},
				expectedStatus: http.StatusBadRequest,
				expectedCode:   "ERR_INVALID_INPUT",
			},
			{
				name:           "Key Invalid Chars",
				payload:        map[string]interface{}{"key": "Invalid Key!", "name": "Bad Chars"},
				expectedStatus: http.StatusBadRequest,
				expectedCode:   "ERR_INVALID_INPUT",
			},
			{
				name:           "Key Wrong Type (Int)",
				payload:        map[string]interface{}{"key": 12345, "name": "Type Error"},
				expectedStatus: http.StatusBadRequest,
				// Fails at JSON Unmarshal level
				expectedCode: "ERR_INVALID_JSON",
			},

			// --- NAME VALIDATION ---
			{
				name:           "Name Missing",
				payload:        map[string]interface{}{"key": "valid-key"},
				expectedStatus: http.StatusBadRequest,
				expectedCode:   "ERR_INVALID_INPUT",
			},
			{
				name:           "Name Too Long",
				payload:        map[string]interface{}{"key": "valid-key", "name": longString},
				expectedStatus: http.StatusBadRequest,
				expectedCode:   "ERR_INVALID_INPUT",
			},
			{
				name:           "Name Wrong Type (Bool)",
				payload:        map[string]interface{}{"key": "valid-key", "name": true},
				expectedStatus: http.StatusBadRequest,
				expectedCode:   "ERR_INVALID_JSON",
			},

			// --- TYPE SAFETY (Other Fields) ---
			{
				name:           "Description Wrong Type",
				payload:        map[string]interface{}{"key": "valid-key", "name": "n1", "description": 123},
				expectedStatus: http.StatusBadRequest,
				expectedCode:   "ERR_INVALID_JSON",
			},
			{
				name:           "Enabled Wrong Type",
				payload:        map[string]interface{}{"key": "valid-key", "name": "n2", "enabled": "false"},
				expectedStatus: http.StatusBadRequest,
				expectedCode:   "ERR_INVALID_JSON",
			},
			{
				name:           "DefaultValue Wrong Type",
				payload:        map[string]interface{}{"key": "valid-key", "name": "n3", "default_value": "true"},
				expectedStatus: http.StatusBadRequest,
				expectedCode:   "ERR_INVALID_JSON",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				body, _ := json.Marshal(tt.payload)
				req := httptest.NewRequest(http.MethodPost, "/api/v1/flags", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				rr := httptest.NewRecorder()

				api.Router.ServeHTTP(rr, req)

				assert.Equal(t, tt.expectedStatus, rr.Code)

				var errResp controlapi.ErrorResponse
				err := json.Unmarshal(rr.Body.Bytes(), &errResp)
				require.NoError(t, err)

				assert.Equal(t, tt.expectedCode, errResp.Code)
				assert.NotEmpty(t, errResp.Message)
			})
		}
	})

	t.Run("POST /flags - Conflict", func(t *testing.T) {
		uniqueKey := fmt.Sprintf("conflict-%d", time.Now().UnixNano())

		// Arrange: Create the initial flag
		original := controlapi.CreateFlagRequest{
			Key:  uniqueKey,
			Name: "Original Flag",
		}

		origBytes, _ := json.Marshal(original)

		setupReq := httptest.NewRequest(http.MethodPost, "/api/v1/flags", bytes.NewReader(origBytes))
		setupReq.Header.Set("Content-Type", "application/json")
		setupRr := httptest.NewRecorder()

		api.Router.ServeHTTP(setupRr, setupReq)

		require.Equal(t, http.StatusCreated, setupRr.Code, "setup failed: could not create original flag")

		// Act: Try to create it again
		duplicate := controlapi.CreateFlagRequest{
			Key:  uniqueKey,
			Name: "Duplicate Attempt",
		}

		dupBytes, _ := json.Marshal(duplicate)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/flags", bytes.NewReader(dupBytes))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		api.Router.ServeHTTP(rr, req)

		// Assert
		assert.Equal(t, http.StatusConflict, rr.Code)

		var errResp controlapi.ErrorResponse
		json.Unmarshal(rr.Body.Bytes(), &errResp)
		assert.Equal(t, "ERR_CONFLICT", errResp.Code)
	})

	// -------------------------------------------------------------------------
	// SCENARIO 2: GET /flags (List & Pagination)
	// -------------------------------------------------------------------------

	t.Run("GET /flags - Pagination Logic", func(t *testing.T) {
		// Arrange: Seed unique data for this test suite
		// We create 15 items.
		prefix := fmt.Sprintf("list-%d", time.Now().UnixNano())
		for i := range 15 {
			f := &store.Flag{
				Key:  fmt.Sprintf("%s-%d", prefix, i),
				Name: "List Test",
			}
			_ = repo.CreateFlag(ctx, f)
		}

		tests := []struct {
			name             string
			query            string
			expectedStatus   int
			expectedPage     int
			expectedSize     int
			expectedItemsLen int
			checkError       bool
		}{
			{
				name:             "No Params (Defaults)",
				query:            "",
				expectedStatus:   http.StatusOK,
				expectedPage:     1,
				expectedSize:     10,
				expectedItemsLen: 10,
			},
			{
				name:             "Custom Page & Size",
				query:            "?page=2&page_size=5",
				expectedStatus:   http.StatusOK,
				expectedPage:     2,
				expectedSize:     5,
				expectedItemsLen: 5,
			},
			{
				name:           "Max Page Size Clamp",
				query:          "?page=1&page_size=1000", // Should clamp to 100
				expectedStatus: http.StatusOK,
				expectedPage:   1,
				expectedSize:   100,
				// We expect at least 15 items (plus any from previous tests),
				// but since 15 < 100, we verify we got more than our seed count.
				expectedItemsLen: 15,
			},
			{
				name:             "Min Page Clamp",
				query:            "?page=-1", // Should clamp to 1
				expectedStatus:   http.StatusOK,
				expectedPage:     1,
				expectedItemsLen: 10,
			},
			{
				name:           "Invalid Page Type",
				query:          "?page=banana",
				expectedStatus: http.StatusBadRequest,
				checkError:     true,
			},
			{
				name:           "Invalid Size Type",
				query:          "?page=1&page_size=true",
				expectedStatus: http.StatusBadRequest,
				checkError:     true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/flags"+tt.query, nil)
				rr := httptest.NewRecorder()

				api.Router.ServeHTTP(rr, req)

				assert.Equal(t, tt.expectedStatus, rr.Code)

				if tt.checkError {
					var errResp controlapi.ErrorResponse
					json.Unmarshal(rr.Body.Bytes(), &errResp)
					assert.Equal(t, "ERR_INVALID_QUERY_PARAM", errResp.Code)
				} else {
					var listResp controlapi.PaginatedResponse
					json.Unmarshal(rr.Body.Bytes(), &listResp)

					// Assert Metadata
					assert.Equal(t, int64(tt.expectedPage), int64(listResp.Pagination.CurrentPage))

					if tt.expectedSize > 0 {
						assert.Equal(t, tt.expectedSize, listResp.Pagination.PageSize)
					}

					// Assert Data Size
					dataSlice, ok := listResp.Data.([]interface{})
					require.True(t, ok, "data field should be an array")

					if tt.name == "Max Page Size Clamp" {
						// For max clamp, we just check if we got at least our seeded data
						assert.GreaterOrEqual(t, len(dataSlice), tt.expectedItemsLen)
					} else {
						assert.Len(t, dataSlice, tt.expectedItemsLen)
					}
				}
			})
		}
	})
}
