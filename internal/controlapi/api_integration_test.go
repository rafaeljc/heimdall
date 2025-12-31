//go:build integration

package controlapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rafaeljc/heimdall/internal/controlapi"
	"github.com/rafaeljc/heimdall/internal/store"
	"github.com/rafaeljc/heimdall/internal/testsupport"
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

	// Start Redis Container
	redisContainer, err := testsupport.StartRedisContainer(ctx)
	require.NoError(t, err, "failed to start redis container")
	defer func() {
		if err := redisContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate redis container: %v", err)
		}
	}()

	// Setup Verification Client (Spy)
	// Even though redisContainer.Client is available, it is the abstract cache.Service interface.
	// To strictly verify the side-effects (queue content) without modifying the interface,
	// we create a raw Redis client just for assertions.
	endpoint, err := redisContainer.Container.PortEndpoint(ctx, "6379/tcp", "")
	require.NoError(t, err, "failed to get redis endpoint for verification")

	verifierClient := redis.NewClient(&redis.Options{Addr: endpoint})
	defer verifierClient.Close()

	// 2. Application Wiring
	// Initialize real dependencies (Repository -> DB)
	repo := store.NewPostgresStore(pgContainer.DB)
	// Initialize API with authentication disabled for integration tests
	api := controlapi.NewAPIWithConfig(repo, redisContainer.Client, "", true)

	// -------------------------------------------------------------------------
	// SCENARIO 1: POST /flags (Creation & Validation)
	// -------------------------------------------------------------------------

	t.Run("POST /flags - Happy Path (Full Payload & Cache Event)", func(t *testing.T) {
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

		// Validate Rules & Versioning
		assert.Equal(t, int64(1), resp.Version, "New flags must start at Version 1")
		assert.JSONEq(t, "[]", string(resp.Rules), "Rules should be an empty JSON array []")

		// Validate Side Effect (Redis Queue)
		// We verify that the API actually pushed the key to the 'heimdall:queue:updates' list.
		require.Eventually(t, func() bool {
			// Check if list has items
			length, err := verifierClient.LLen(ctx, "heimdall:queue:updates").Result()
			if err != nil || length == 0 {
				return false
			}

			// Check if the item is indeed our key
			// Note: In a real concurent test we might just check LPos or LRange,
			// but for this isolated test, LPop is fine.
			val, err := verifierClient.LPop(ctx, "heimdall:queue:updates").Result()
			return err == nil && val == key
		}, 2*time.Second, 100*time.Millisecond, "Flag key must appear in Redis update queue")
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
		assert.Equal(t, int64(1), resp.Version)
		assert.JSONEq(t, "[]", string(resp.Rules))
	})

	t.Run("POST /flags - Validation & Type Safety", func(t *testing.T) {
		longString := strings.Repeat("a", 256)

		tests := []struct {
			name           string
			payload        map[string]any
			expectedStatus int
			expectedCode   string
		}{
			// --- KEY VALIDATION ---
			{
				name:           "Key Missing",
				payload:        map[string]any{"name": "No Key"},
				expectedStatus: http.StatusBadRequest,
				expectedCode:   "ERR_INVALID_INPUT",
			},
			{
				name:           "Key Too Short",
				payload:        map[string]any{"key": "ab", "name": "Short"},
				expectedStatus: http.StatusBadRequest,
				expectedCode:   "ERR_INVALID_INPUT",
			},
			{
				name:           "Key Too Long",
				payload:        map[string]any{"key": longString, "name": "Long"},
				expectedStatus: http.StatusBadRequest,
				expectedCode:   "ERR_INVALID_INPUT",
			},
			{
				name:           "Key Invalid Chars",
				payload:        map[string]any{"key": "Invalid Key!", "name": "Bad Chars"},
				expectedStatus: http.StatusBadRequest,
				expectedCode:   "ERR_INVALID_INPUT",
			},
			{
				name:           "Key Wrong Type (Int)",
				payload:        map[string]any{"key": 12345, "name": "Type Error"},
				expectedStatus: http.StatusBadRequest,
				// Fails at JSON Unmarshal level
				expectedCode: "ERR_INVALID_JSON",
			},

			// --- NAME VALIDATION ---
			{
				name:           "Name Missing",
				payload:        map[string]any{"key": "valid-key"},
				expectedStatus: http.StatusBadRequest,
				expectedCode:   "ERR_INVALID_INPUT",
			},
			{
				name:           "Name Too Long",
				payload:        map[string]any{"key": "valid-key", "name": longString},
				expectedStatus: http.StatusBadRequest,
				expectedCode:   "ERR_INVALID_INPUT",
			},
			{
				name:           "Name Wrong Type (Bool)",
				payload:        map[string]any{"key": "valid-key", "name": true},
				expectedStatus: http.StatusBadRequest,
				expectedCode:   "ERR_INVALID_JSON",
			},

			// --- TYPE SAFETY (Other Fields) ---
			{
				name:           "Description Wrong Type",
				payload:        map[string]any{"key": "valid-key", "name": "n1", "description": 123},
				expectedStatus: http.StatusBadRequest,
				expectedCode:   "ERR_INVALID_JSON",
			},
			{
				name:           "Enabled Wrong Type",
				payload:        map[string]any{"key": "valid-key", "name": "n2", "enabled": "false"},
				expectedStatus: http.StatusBadRequest,
				expectedCode:   "ERR_INVALID_JSON",
			},
			{
				name:           "DefaultValue Wrong Type",
				payload:        map[string]any{"key": "valid-key", "name": "n3", "default_value": "true"},
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

	// -------------------------------------------------------------------------
	// SCENARIO 3: GET /flags/{key} (Single Flag Retrieval)
	// -------------------------------------------------------------------------

	t.Run("GET /flags/{key} - Happy Path", func(t *testing.T) {
		// Arrange: Create a flag with full data including rules
		key := fmt.Sprintf("get-happy-%d", time.Now().UnixNano())

		createReq := controlapi.CreateFlagRequest{
			Key:          key,
			Name:         "Get Happy Flag",
			Description:  "Test flag for GET endpoint",
			Enabled:      true,
			DefaultValue: true,
		}

		createBody, _ := json.Marshal(createReq)
		createHTTPReq := httptest.NewRequest(http.MethodPost, "/api/v1/flags", bytes.NewReader(createBody))
		createHTTPReq.Header.Set("Content-Type", "application/json")
		createRR := httptest.NewRecorder()
		api.Router.ServeHTTP(createRR, createHTTPReq)
		require.Equal(t, http.StatusCreated, createRR.Code)

		// Act: Retrieve the flag
		req := httptest.NewRequest(http.MethodGet, "/api/v1/flags/"+key, nil)
		rr := httptest.NewRecorder()
		api.Router.ServeHTTP(rr, req)

		// Assert: HTTP Status
		require.Equal(t, http.StatusOK, rr.Code)

		// Assert: Response Structure
		var resp controlapi.Flag
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))

		// Assert: Data Integrity
		assert.Equal(t, key, resp.Key)
		assert.Equal(t, "Get Happy Flag", resp.Name)
		assert.Equal(t, "Test flag for GET endpoint", resp.Description)
		assert.True(t, resp.Enabled)
		assert.True(t, resp.DefaultValue)
		assert.Equal(t, int64(1), resp.Version)
		assert.NotZero(t, resp.ID)
		assert.False(t, resp.CreatedAt.IsZero())
		assert.False(t, resp.UpdatedAt.IsZero())

		// Assert: Rules are empty array by default
		assert.NotNil(t, resp.Rules)
		assert.JSONEq(t, "[]", string(resp.Rules))
	})

	t.Run("GET /flags/{key} - Not Found", func(t *testing.T) {
		// According to OpenAPI spec, GET /flags/{key} returns 404 for any non-existent key,
		// regardless of whether the key format is valid or invalid.
		// The spec only defines 200 (success) and 404 (not found) status codes.

		// Arrange: Create a flag to ensure at least one valid key exists
		existingKey := fmt.Sprintf("existing-%d", time.Now().UnixNano())
		createReq := controlapi.CreateFlagRequest{
			Key:  existingKey,
			Name: "Existing Flag",
		}
		createBody, _ := json.Marshal(createReq)
		createHTTPReq := httptest.NewRequest(http.MethodPost, "/api/v1/flags", bytes.NewReader(createBody))
		createHTTPReq.Header.Set("Content-Type", "application/json")
		createRR := httptest.NewRecorder()
		api.Router.ServeHTTP(createRR, createHTTPReq)
		require.Equal(t, http.StatusCreated, createRR.Code)

		tests := []struct {
			name string
			key  string
		}{
			{"Valid Format But Nonexistent", fmt.Sprintf("non-existent-%d", time.Now().UnixNano())},
			{"Invalid Format - Too Short", "ab"},
			{"Invalid Format - Uppercase", strings.ToUpper(existingKey)},
			{"Invalid Format - Special Chars", "invalid@key!"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// Act
				req := httptest.NewRequest(http.MethodGet, "/api/v1/flags/"+tt.key, nil)
				rr := httptest.NewRecorder()
				api.Router.ServeHTTP(rr, req)

				// Assert: All non-existent keys return 404, not 400
				assert.Equal(t, http.StatusNotFound, rr.Code)

				var errResp controlapi.ErrorResponse
				require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &errResp))
				assert.Equal(t, "ERR_NOT_FOUND", errResp.Code)
				assert.Contains(t, errResp.Message, tt.key)
			})
		}
	})

	// -------------------------------------------------------------------------
	// SCENARIO 4: DELETE /flags/{key} (Flag Deletion)
	// -------------------------------------------------------------------------

	t.Run("DELETE /flags/{key} - Happy Path", func(t *testing.T) {
		// Arrange: Create a flag to delete
		key := fmt.Sprintf("delete-happy-%d", time.Now().UnixNano())

		createReq := controlapi.CreateFlagRequest{
			Key:          key,
			Name:         "Flag to Delete",
			Description:  "Test deletion",
			Enabled:      true,
			DefaultValue: false,
		}

		createBody, _ := json.Marshal(createReq)
		createHTTPReq := httptest.NewRequest(http.MethodPost, "/api/v1/flags", bytes.NewReader(createBody))
		createHTTPReq.Header.Set("Content-Type", "application/json")
		createRR := httptest.NewRecorder()
		api.Router.ServeHTTP(createRR, createHTTPReq)
		require.Equal(t, http.StatusCreated, createRR.Code)

		// Act: Delete the flag
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/flags/"+key, nil)
		rr := httptest.NewRecorder()
		api.Router.ServeHTTP(rr, req)

		// Assert: HTTP Status
		require.Equal(t, http.StatusNoContent, rr.Code)
		assert.Empty(t, rr.Body.String(), "204 No Content must have empty body")

		// Assert: Side Effect - Flag no longer exists in database
		getReq := httptest.NewRequest(http.MethodGet, "/api/v1/flags/"+key, nil)
		getRR := httptest.NewRecorder()
		api.Router.ServeHTTP(getRR, getReq)
		assert.Equal(t, http.StatusNotFound, getRR.Code, "Deleted flag should return 404")
	})

	t.Run("DELETE /flags/{key} - Not Found", func(t *testing.T) {
		// Arrange: Generate a key that doesn't exist
		nonExistentKey := fmt.Sprintf("non-existent-%d", time.Now().UnixNano())

		// Act
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/flags/"+nonExistentKey, nil)
		rr := httptest.NewRecorder()
		api.Router.ServeHTTP(rr, req)

		// Assert: HTTP Status
		require.Equal(t, http.StatusNotFound, rr.Code)

		// Assert: Error Response Structure
		var errResp controlapi.ErrorResponse
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &errResp))
		assert.Equal(t, "ERR_NOT_FOUND", errResp.Code)
		assert.Contains(t, errResp.Message, nonExistentKey)
	})

	t.Run("DELETE /flags/{key} - Cache Event Published", func(t *testing.T) {
		// Arrange: Create a flag and clear any existing queue events
		key := fmt.Sprintf("delete-cache-%d", time.Now().UnixNano())

		createReq := controlapi.CreateFlagRequest{
			Key:          key,
			Name:         "Delete Cache Test",
			Enabled:      true,
			DefaultValue: false,
		}

		createBody, _ := json.Marshal(createReq)
		createHTTPReq := httptest.NewRequest(http.MethodPost, "/api/v1/flags", bytes.NewReader(createBody))
		createHTTPReq.Header.Set("Content-Type", "application/json")
		createRR := httptest.NewRecorder()
		api.Router.ServeHTTP(createRR, createHTTPReq)
		require.Equal(t, http.StatusCreated, createRR.Code)

		// Clear the queue to isolate the delete event
		verifierClient.Del(ctx, "heimdall:queue:updates")

		// Act: Delete the flag
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/flags/"+key, nil)
		rr := httptest.NewRecorder()
		api.Router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusNoContent, rr.Code)

		// Assert: Side Effect - Cache event was published to Redis queue
		// We verify that the API actually pushed the key to the 'heimdall:queue:updates' list.
		require.Eventually(t, func() bool {
			// Check if list has items
			length, err := verifierClient.LLen(ctx, "heimdall:queue:updates").Result()
			if err != nil || length == 0 {
				return false
			}

			// Check if the item is indeed our key
			val, err := verifierClient.LPop(ctx, "heimdall:queue:updates").Result()
			return err == nil && val == key
		}, 2*time.Second, 100*time.Millisecond, "Flag key must appear in Redis update queue")
	})

	// -------------------------------------------------------------------------
	// SCENARIO 5: PATCH /flags/{key} (Flag Update)
	// -------------------------------------------------------------------------

	t.Run("PATCH /flags/{key} - Happy Path (Partial Update)", func(t *testing.T) {
		// Arrange: Create a flag to update
		key := fmt.Sprintf("update-happy-%d", time.Now().UnixNano())

		createReq := controlapi.CreateFlagRequest{
			Key:          key,
			Name:         "Original Name",
			Description:  "Original Description",
			Enabled:      false,
			DefaultValue: false,
		}

		createBody, _ := json.Marshal(createReq)
		createHTTPReq := httptest.NewRequest(http.MethodPost, "/api/v1/flags", bytes.NewReader(createBody))
		createHTTPReq.Header.Set("Content-Type", "application/json")
		createRR := httptest.NewRecorder()
		api.Router.ServeHTTP(createRR, createHTTPReq)
		require.Equal(t, http.StatusCreated, createRR.Code)

		var createdFlag controlapi.Flag
		json.Unmarshal(createRR.Body.Bytes(), &createdFlag)
		assert.Equal(t, int64(1), createdFlag.Version)

		// Act: Update only name and enabled fields
		newName := "Updated Name"
		newEnabled := true
		updateReq := map[string]interface{}{
			"name":    newName,
			"enabled": newEnabled,
		}

		updateBody, _ := json.Marshal(updateReq)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/flags/"+key, bytes.NewReader(updateBody))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		api.Router.ServeHTTP(rr, req)

		// Assert: HTTP Status
		require.Equal(t, http.StatusOK, rr.Code)

		// Assert: Response Structure
		var resp controlapi.Flag
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))

		// Assert: Updated fields changed
		assert.Equal(t, newName, resp.Name)
		assert.True(t, resp.Enabled)

		// Assert: Non-updated fields preserved
		assert.Equal(t, "Original Description", resp.Description)
		assert.False(t, resp.DefaultValue)

		// Assert: Version incremented
		assert.Equal(t, int64(2), resp.Version)

		// Assert: Timestamps
		assert.False(t, resp.UpdatedAt.IsZero())
		assert.True(t, resp.UpdatedAt.After(createdFlag.UpdatedAt))
	})

	t.Run("PATCH /flags/{key} - Not Found", func(t *testing.T) {
		// Arrange: Generate a key that doesn't exist
		nonExistentKey := fmt.Sprintf("non-existent-%d", time.Now().UnixNano())

		updateReq := map[string]interface{}{
			"name": "New Name",
		}

		// Act
		updateBody, _ := json.Marshal(updateReq)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/flags/"+nonExistentKey, bytes.NewReader(updateBody))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		api.Router.ServeHTTP(rr, req)

		// Assert: HTTP Status
		require.Equal(t, http.StatusNotFound, rr.Code)

		// Assert: Error Response Structure
		var errResp controlapi.ErrorResponse
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &errResp))
		assert.Equal(t, "ERR_NOT_FOUND", errResp.Code)
		assert.Contains(t, errResp.Message, nonExistentKey)
	})

	t.Run("PATCH /flags/{key} - Validation Errors", func(t *testing.T) {
		// Arrange: Create a flag to update
		key := fmt.Sprintf("update-validation-%d", time.Now().UnixNano())

		createReq := controlapi.CreateFlagRequest{
			Key:  key,
			Name: "Test Flag",
		}

		createBody, _ := json.Marshal(createReq)
		createHTTPReq := httptest.NewRequest(http.MethodPost, "/api/v1/flags", bytes.NewReader(createBody))
		createHTTPReq.Header.Set("Content-Type", "application/json")
		createRR := httptest.NewRecorder()
		api.Router.ServeHTTP(createRR, createHTTPReq)
		require.Equal(t, http.StatusCreated, createRR.Code)

		tests := []struct {
			name           string
			payload        map[string]interface{}
			expectedStatus int
			expectedCode   string
		}{
			{
				name:           "Empty name",
				payload:        map[string]interface{}{"name": ""},
				expectedStatus: http.StatusBadRequest,
				expectedCode:   "ERR_INVALID_INPUT",
			},
			{
				name:           "Name too long",
				payload:        map[string]interface{}{"name": strings.Repeat("a", 256)},
				expectedStatus: http.StatusBadRequest,
				expectedCode:   "ERR_INVALID_INPUT",
			},
			{
				name:           "Invalid type for name",
				payload:        map[string]interface{}{"name": 123},
				expectedStatus: http.StatusBadRequest,
				expectedCode:   "ERR_INVALID_JSON",
			},
			{
				name:           "Invalid type for enabled",
				payload:        map[string]interface{}{"enabled": "true"},
				expectedStatus: http.StatusBadRequest,
				expectedCode:   "ERR_INVALID_JSON",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				body, _ := json.Marshal(tt.payload)
				req := httptest.NewRequest(http.MethodPatch, "/api/v1/flags/"+key, bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				rr := httptest.NewRecorder()
				api.Router.ServeHTTP(rr, req)

				assert.Equal(t, tt.expectedStatus, rr.Code)

				var errResp controlapi.ErrorResponse
				json.Unmarshal(rr.Body.Bytes(), &errResp)
				assert.Equal(t, tt.expectedCode, errResp.Code)
			})
		}
	})

	t.Run("PATCH /flags/{key} - Cache Event Published", func(t *testing.T) {
		// Arrange: Create a flag to update
		key := fmt.Sprintf("update-cache-%d", time.Now().UnixNano())

		createReq := controlapi.CreateFlagRequest{
			Key:     key,
			Name:    "Cache Test",
			Enabled: false,
		}

		createBody, _ := json.Marshal(createReq)
		createHTTPReq := httptest.NewRequest(http.MethodPost, "/api/v1/flags", bytes.NewReader(createBody))
		createHTTPReq.Header.Set("Content-Type", "application/json")
		createRR := httptest.NewRecorder()
		api.Router.ServeHTTP(createRR, createHTTPReq)
		require.Equal(t, http.StatusCreated, createRR.Code)

		// Clear the queue to isolate the update event
		verifierClient.Del(ctx, "heimdall:queue:updates")

		// Act: Update the flag
		newEnabled := true
		updateReq := map[string]interface{}{
			"enabled": newEnabled,
		}

		updateBody, _ := json.Marshal(updateReq)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/flags/"+key, bytes.NewReader(updateBody))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		api.Router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)

		// Assert: Side Effect - Cache event was published to Redis queue
		require.Eventually(t, func() bool {
			// Check if list has items
			length, err := verifierClient.LLen(ctx, "heimdall:queue:updates").Result()
			if err != nil || length == 0 {
				return false
			}

			// Check if the item is indeed our key
			val, err := verifierClient.LPop(ctx, "heimdall:queue:updates").Result()
			return err == nil && val == key
		}, 2*time.Second, 100*time.Millisecond, "Flag key must appear in Redis update queue")
	})
}

// TestNewAPIValidation_Integration validates the factory method validation behavior.
// It ensures that constructors properly validate input parameters and panic with appropriate messages.
func TestNewAPIValidation_Integration(t *testing.T) {
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

	// Start Redis Container
	redisContainer, err := testsupport.StartRedisContainer(ctx)
	require.NoError(t, err, "failed to start redis container")
	defer func() {
		if err := redisContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate redis container: %v", err)
		}
	}()

	// 2. Application Wiring
	repo := store.NewPostgresStore(pgContainer.DB)

	// 3. Validation Tests

	t.Run("NewAPIWithConfig should panic when flagRepo is nil", func(t *testing.T) {
		assert.PanicsWithValue(t, "controlapi: flag repository cannot be nil", func() {
			controlapi.NewAPIWithConfig(nil, redisContainer.Client, "valid-hash", false)
		}, "Should panic when flagRepo is nil")
	})

	t.Run("NewAPIWithConfig should panic when cacheSvc is nil", func(t *testing.T) {
		assert.PanicsWithValue(t, "controlapi: cache service cannot be nil", func() {
			controlapi.NewAPIWithConfig(repo, nil, "valid-hash", false)
		}, "Should panic when cacheSvc is nil")
	})

	t.Run("NewAPIWithConfig should panic when apiKeyHash is empty and skipAuth is false", func(t *testing.T) {
		assert.PanicsWithValue(t, "controlapi: apiKeyHash cannot be empty when authentication is enabled", func() {
			controlapi.NewAPIWithConfig(repo, redisContainer.Client, "", false)
		}, "Should panic when authentication is enabled but apiKeyHash is empty")
	})

	t.Run("NewAPI should panic when apiKeyHash is empty", func(t *testing.T) {
		assert.PanicsWithValue(t, "controlapi: apiKeyHash cannot be empty when authentication is enabled", func() {
			controlapi.NewAPI(repo, redisContainer.Client, "")
		}, "NewAPI should panic when apiKeyHash is empty since it always enables authentication")
	})
}

// TestAuthentication_Integration validates the X-API-Key authentication middleware.
// It ensures that protected endpoints require valid authentication and public endpoints remain accessible.
func TestAuthentication_Integration(t *testing.T) {
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

	// Start Redis Container
	redisContainer, err := testsupport.StartRedisContainer(ctx)
	require.NoError(t, err, "failed to start redis container")
	defer func() {
		if err := redisContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate redis container: %v", err)
		}
	}()

	// 2. Application Wiring
	repo := store.NewPostgresStore(pgContainer.DB)

	// Generate test API key and its hash
	testAPIKey := "test-api-key-12345"
	hash := sha256.Sum256([]byte(testAPIKey))
	testAPIKeyHash := hex.EncodeToString(hash[:])

	// Initialize API with authentication enabled
	api := controlapi.NewAPIWithConfig(repo, redisContainer.Client, testAPIKeyHash, false)

	// 3. Authentication Tests

	t.Run("GET /health should be accessible without authentication", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rr := httptest.NewRecorder()

		api.Router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code, "Health endpoint should be public")
	})

	t.Run("GET /api/v1/flags should reject request without X-API-Key header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/flags", nil)
		rr := httptest.NewRecorder()

		api.Router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code, "Should return 401 Unauthorized")

		var errResp controlapi.ErrorResponse
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &errResp))
		assert.Equal(t, "missing X-API-Key header", errResp.Message)
	})

	t.Run("GET /api/v1/flags should reject request with invalid X-API-Key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/flags", nil)
		req.Header.Set("X-API-Key", "invalid-key")
		rr := httptest.NewRecorder()

		api.Router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code, "Should return 401 Unauthorized")

		var errResp controlapi.ErrorResponse
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &errResp))
		assert.Equal(t, "invalid API key", errResp.Message)
	})

	t.Run("GET /api/v1/flags should accept request with valid X-API-Key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/flags", nil)
		req.Header.Set("X-API-Key", testAPIKey)
		rr := httptest.NewRecorder()

		api.Router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code, "Should return 200 OK with valid API key")
	})

	t.Run("GET /api/v1/flags/{key} should reject request without X-API-Key header", func(t *testing.T) {
		// First, create a flag to retrieve (with valid auth)
		key := fmt.Sprintf("get-auth-test-%d", time.Now().UnixNano())
		createInput := controlapi.CreateFlagRequest{
			Key:          key,
			Name:         "Get Auth Test",
			Description:  "Test",
			Enabled:      true,
			DefaultValue: true,
		}
		createBody, _ := json.Marshal(createInput)
		createReq := httptest.NewRequest(http.MethodPost, "/api/v1/flags", bytes.NewReader(createBody))
		createReq.Header.Set("Content-Type", "application/json")
		createReq.Header.Set("X-API-Key", testAPIKey)
		createRR := httptest.NewRecorder()
		api.Router.ServeHTTP(createRR, createReq)
		require.Equal(t, http.StatusCreated, createRR.Code)

		// Try to retrieve without authentication
		req := httptest.NewRequest(http.MethodGet, "/api/v1/flags/"+key, nil)
		rr := httptest.NewRecorder()

		api.Router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code, "Should return 401 Unauthorized")
	})

	t.Run("GET /api/v1/flags/{key} should accept request with valid X-API-Key", func(t *testing.T) {
		// First, create a flag to retrieve (with valid auth)
		key := fmt.Sprintf("get-auth-happy-%d", time.Now().UnixNano())
		createInput := controlapi.CreateFlagRequest{
			Key:          key,
			Name:         "Get Auth Happy Test",
			Description:  "Test description",
			Enabled:      true,
			DefaultValue: true,
		}
		createBody, _ := json.Marshal(createInput)
		createReq := httptest.NewRequest(http.MethodPost, "/api/v1/flags", bytes.NewReader(createBody))
		createReq.Header.Set("Content-Type", "application/json")
		createReq.Header.Set("X-API-Key", testAPIKey)
		createRR := httptest.NewRecorder()
		api.Router.ServeHTTP(createRR, createReq)
		require.Equal(t, http.StatusCreated, createRR.Code)

		// Retrieve with authentication
		req := httptest.NewRequest(http.MethodGet, "/api/v1/flags/"+key, nil)
		req.Header.Set("X-API-Key", testAPIKey)
		rr := httptest.NewRecorder()

		api.Router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code, "Should return 200 OK with valid API key")
	})

	t.Run("POST /api/v1/flags should reject request without X-API-Key header", func(t *testing.T) {
		input := controlapi.CreateFlagRequest{
			Key:          "test-flag",
			Name:         "Test Flag",
			Description:  "Test",
			Enabled:      true,
			DefaultValue: true,
		}
		body, _ := json.Marshal(input)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/flags", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		api.Router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code, "Should return 401 Unauthorized")
	})

	t.Run("POST /api/v1/flags should accept request with valid X-API-Key", func(t *testing.T) {
		key := fmt.Sprintf("auth-test-flag-%d", time.Now().UnixNano())
		input := controlapi.CreateFlagRequest{
			Key:          key,
			Name:         "Auth Test Flag",
			Description:  "Test",
			Enabled:      true,
			DefaultValue: true,
		}
		body, _ := json.Marshal(input)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/flags", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", testAPIKey)
		rr := httptest.NewRecorder()

		api.Router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusCreated, rr.Code, "Should return 201 Created with valid API key")
	})

	t.Run("PATCH /api/v1/flags/{key} should require authentication", func(t *testing.T) {
		// First, create a flag to update (with valid auth)
		key := fmt.Sprintf("patch-auth-test-%d", time.Now().UnixNano())
		createInput := controlapi.CreateFlagRequest{
			Key:          key,
			Name:         "Patch Auth Test",
			Description:  "Test",
			Enabled:      true,
			DefaultValue: true,
		}
		createBody, _ := json.Marshal(createInput)
		createReq := httptest.NewRequest(http.MethodPost, "/api/v1/flags", bytes.NewReader(createBody))
		createReq.Header.Set("Content-Type", "application/json")
		createReq.Header.Set("X-API-Key", testAPIKey)
		createRR := httptest.NewRecorder()
		api.Router.ServeHTTP(createRR, createReq)
		require.Equal(t, http.StatusCreated, createRR.Code)

		// Try to update without authentication
		updateInput := controlapi.UpdateFlagRequest{
			Name: strPtr("Updated Name"),
		}
		updateBody, _ := json.Marshal(updateInput)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/flags/"+key, bytes.NewReader(updateBody))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		api.Router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code, "Should return 401 Unauthorized")
	})

	t.Run("PATCH /api/v1/flags/{key} should accept request with valid X-API-Key", func(t *testing.T) {
		// First, create a flag to update (with valid auth)
		key := fmt.Sprintf("patch-auth-happy-%d", time.Now().UnixNano())
		createInput := controlapi.CreateFlagRequest{
			Key:          key,
			Name:         "Patch Auth Happy Test",
			Description:  "Original description",
			Enabled:      true,
			DefaultValue: true,
		}
		createBody, _ := json.Marshal(createInput)
		createReq := httptest.NewRequest(http.MethodPost, "/api/v1/flags", bytes.NewReader(createBody))
		createReq.Header.Set("Content-Type", "application/json")
		createReq.Header.Set("X-API-Key", testAPIKey)
		createRR := httptest.NewRecorder()
		api.Router.ServeHTTP(createRR, createReq)
		require.Equal(t, http.StatusCreated, createRR.Code)

		// Update with authentication
		updateInput := controlapi.UpdateFlagRequest{
			Name:        strPtr("Updated Name"),
			Description: strPtr("Updated description"),
			Enabled:     boolPtr(false),
		}
		updateBody, _ := json.Marshal(updateInput)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/flags/"+key, bytes.NewReader(updateBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", testAPIKey)
		rr := httptest.NewRecorder()

		api.Router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code, "Should return 200 OK with valid API key")
	})

	t.Run("DELETE /api/v1/flags/{key} should require authentication", func(t *testing.T) {
		// First, create a flag to delete (with valid auth)
		key := fmt.Sprintf("delete-auth-test-%d", time.Now().UnixNano())
		createInput := controlapi.CreateFlagRequest{
			Key:          key,
			Name:         "Delete Auth Test",
			Description:  "Test",
			Enabled:      true,
			DefaultValue: true,
		}
		createBody, _ := json.Marshal(createInput)
		createReq := httptest.NewRequest(http.MethodPost, "/api/v1/flags", bytes.NewReader(createBody))
		createReq.Header.Set("Content-Type", "application/json")
		createReq.Header.Set("X-API-Key", testAPIKey)
		createRR := httptest.NewRecorder()
		api.Router.ServeHTTP(createRR, createReq)
		require.Equal(t, http.StatusCreated, createRR.Code)

		// Try to delete without authentication
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/flags/"+key, nil)
		rr := httptest.NewRecorder()

		api.Router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code, "Should return 401 Unauthorized")
	})

	t.Run("DELETE /api/v1/flags/{key} should accept request with valid X-API-Key", func(t *testing.T) {
		// First, create a flag to delete (with valid auth)
		key := fmt.Sprintf("delete-auth-happy-%d", time.Now().UnixNano())
		createInput := controlapi.CreateFlagRequest{
			Key:          key,
			Name:         "Delete Auth Happy Test",
			Description:  "Test",
			Enabled:      true,
			DefaultValue: true,
		}
		createBody, _ := json.Marshal(createInput)
		createReq := httptest.NewRequest(http.MethodPost, "/api/v1/flags", bytes.NewReader(createBody))
		createReq.Header.Set("Content-Type", "application/json")
		createReq.Header.Set("X-API-Key", testAPIKey)
		createRR := httptest.NewRecorder()
		api.Router.ServeHTTP(createRR, createReq)
		require.Equal(t, http.StatusCreated, createRR.Code)

		// Delete with authentication
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/flags/"+key, nil)
		req.Header.Set("X-API-Key", testAPIKey)
		rr := httptest.NewRecorder()

		api.Router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusNoContent, rr.Code, "Should return 204 No Content with valid API key")
	})
}

// Helper functions for pointer values
func strPtr(s string) *string {
	return &s
}

func boolPtr(b bool) *bool {
	return &b
}
