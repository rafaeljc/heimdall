package controlapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/rafaeljc/heimdall/internal/logger"
	"github.com/rafaeljc/heimdall/internal/ruleengine"
	"github.com/rafaeljc/heimdall/internal/store"
)

// handleCreateFlag processes the POST /api/v1/flags request.
//
// Responsibilities:
// 1. Decodes the JSON payload into the CreateFlagRequest DTO.
// 2. Validates the input using the DTO's business logic.
// 3. Converts the DTO to the domain model (store.Flag).
// 4. Persists the flag using the Repository layer.
// 5. Handles specific persistence errors (e.g., conflicts).
// 6. Returns the created resource with a 201 Created status.
func (a *API) handleCreateFlag(w http.ResponseWriter, r *http.Request) {
	log := logger.FromContext(r.Context())

	// 1. Decode Request
	var req CreateFlagRequest
	if err := render.DecodeJSON(r.Body, &req); err != nil {
		log.Warn("invalid json payload", slog.String("error", err.Error()))
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, ErrorResponse{
			Code:    "ERR_INVALID_JSON",
			Message: "Invalid JSON payload: " + err.Error(),
		})
		return
	}

	// 2. Validate
	// We delegate this logic to the DTO to keep the handler clean and testable.
	if errResp := req.Validate(); errResp != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, errResp)
		return
	}

	// 3. Map DTO to Domain Model
	// We explicitly map fields to avoid coupling the API contract directly to the DB schema.
	var rules []ruleengine.Rule
	if len(req.Rules) > 0 {
		// Validation has already passed, so unmarshal should be safe,
		// but we check just in case.
		if err := json.Unmarshal(req.Rules, &rules); err != nil {
			log.Warn("failed to parse rules", slog.String("error", err.Error()))
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, ErrorResponse{
				Code:    "ERR_INVALID_RULES",
				Message: "Failed to parse targeting rules",
			})
			return
		}
	}

	flag := &store.Flag{
		Key:          req.Key,
		Name:         req.Name,
		Description:  req.Description,
		Enabled:      req.Enabled,
		DefaultValue: req.DefaultValue,
		Rules:        rules,
	}

	// 4. Call Repository (Persistence)
	if err := a.flags.CreateFlag(r.Context(), flag); err != nil {
		// Business Error: Conflict (Duplicate Key)
		// Ideally, the store should return a typed error (store.ErrDuplicate),
		// but checking the error string is acceptable for this V1 milestone.
		if strings.Contains(err.Error(), "already exists") {
			render.Status(r, http.StatusConflict)
			render.JSON(w, r, ErrorResponse{
				Code:    "ERR_CONFLICT",
				Message: "A flag with this key already exists",
			})
			return
		}

		// System Error: Internal Server Error
		log.Error("failed to create flag in db", "error", err)
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, ErrorResponse{
			Code:    "ERR_INTERNAL",
			Message: "Failed to create flag in database",
		})
		return
	}

	// 5. Map Domain Model back to Response DTO
	resp := mapStoreFlagToResponse(flag)

	// 6. Async Notification
	a.notifyCacheAsync(log, flag.Key, flag.Version)

	// 7. Set Response Headers
	setETagHeaders(w, flag.Version)

	// 8. Return Success
	log.Info("flag created successfully", slog.String("flag_key", flag.Key), slog.Int64("flag_id", flag.ID))
	render.Status(r, http.StatusCreated)
	render.JSON(w, r, resp)
}

// handleListFlags processes the GET /api/v1/flags request.
//
// Responsibilities:
// 1. Parses and validates pagination parameters (page, page_size).
// 2. Calls the Repository to fetch data and total count.
// 3. Maps domain models to DTOs.
// 4. Calculates pagination metadata (total pages).
// 5. Returns the PaginatedResponse.
func (a *API) handleListFlags(w http.ResponseWriter, r *http.Request) {
	log := logger.FromContext(r.Context())

	// 1. Parse Query Parameters (Type Validation)
	// We return 400 Bad Request if the user sends invalid types (e.g., page=banana).
	page, err := parseOptionalInt(r, "page", 1)
	if err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, ErrorResponse{
			Code:    "ERR_INVALID_QUERY_PARAM",
			Message: err.Error(),
		})
		return
	}

	pageSize, err := parseOptionalInt(r, "page_size", 10)
	if err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, ErrorResponse{
			Code:    "ERR_INVALID_QUERY_PARAM",
			Message: err.Error(),
		})
		return
	}

	// 2. Clamp (Logic Validation)
	// We silently correct out-of-bounds values to ensure system stability and UX.
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100 // Hard limit to prevent large queries
	}

	// 3. Calculate Offset
	offset := (page - 1) * pageSize

	// 4. Call Repository
	flags, totalItems, err := a.flags.ListFlags(r.Context(), pageSize, offset)
	if err != nil {
		log.Error("failed to list flags from db", slog.String("error", err.Error()))

		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, ErrorResponse{
			Code:    "ERR_INTERNAL",
			Message: "Failed to list flags",
		})
		return
	}

	// 5. Map to DTOs
	dtos := make([]Flag, len(flags))
	for i, f := range flags {
		dtos[i] = mapStoreFlagToResponse(f)
	}

	// 6. Calculate Metadata
	totalPages := 0
	if totalItems > 0 {
		totalPages = int(math.Ceil(float64(totalItems) / float64(pageSize)))
	}

	// 7. Build Response
	resp := PaginatedResponse{
		Data: dtos,
		Pagination: Pagination{
			TotalItems:  totalItems,
			TotalPages:  totalPages,
			CurrentPage: page,
			PageSize:    pageSize,
		},
	}

	render.Status(r, http.StatusOK)
	render.JSON(w, r, resp)
}

// handleGetFlag processes the GET /api/v1/flags/{key} request.
//
// Responsibilities:
// 1. Extracts the {key} path parameter.
// 2. Calls the Repository to fetch the flag by key.
// 3. Handles not found errors (404).
// 4. Maps the domain model to the response DTO.
// 5. Returns the flag with a 200 OK status.
//
// Note: We do NOT validate the key format here. According to the OpenAPI spec,
// GET /flags/{key} only returns 200 or 404. If the key format is invalid, the
// database query will simply not find it, resulting in a 404 (which is correct).
func (a *API) handleGetFlag(w http.ResponseWriter, r *http.Request) {
	log := logger.FromContext(r.Context())

	// 1. Extract Path Parameter
	// Chi automatically handles URL decoding for us.
	key := chi.URLParam(r, "key")

	// 2. Call Repository
	flag, err := a.flags.GetFlagByKey(r.Context(), key)
	if err != nil {
		// Check if it's a "not found" error
		// The store returns an error containing "not found" in the message.
		if strings.Contains(err.Error(), "not found") {
			render.Status(r, http.StatusNotFound)
			render.JSON(w, r, ErrorResponse{
				Code:    "ERR_NOT_FOUND",
				Message: fmt.Sprintf("Flag with key '%s' not found", key),
			})
			return
		}

		// System Error: Internal Server Error
		log.Error("failed to get flag from db", slog.String("key", key), slog.String("error", err.Error()))
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, ErrorResponse{
			Code:    "ERR_INTERNAL",
			Message: "Failed to retrieve flag",
		})
		return
	}

	// 4. Map to Response DTO
	resp := mapStoreFlagToResponse(flag)

	// 5. Set Response Headers
	setETagHeaders(w, flag.Version)

	// 6. Return Success
	render.Status(r, http.StatusOK)
	render.JSON(w, r, resp)
}

// handleDeleteFlag processes the DELETE /api/v1/flags/{key} request.
//
// Responsibilities:
// 1. Extracts the {key} path parameter.
// 2. Validates the If-Match header and parses the version.
// 3. Calls the Repository to delete the flag with optimistic locking.
// 4. Handles not found and version conflict errors (404, 412).
// 5. Notifies cache asynchronously to invalidate the flag.
// 6. Returns 204 No Content on success.
func (a *API) handleDeleteFlag(w http.ResponseWriter, r *http.Request) {
	log := logger.FromContext(r.Context())

	// 1. Extract Path Parameter
	key := chi.URLParam(r, "key")

	// 2. Validate If-Match Header and Parse Version
	version, errResp := requireIfMatch(r)
	if errResp != nil {
		render.Status(r, http.StatusPreconditionRequired)
		render.JSON(w, r, errResp)
		return
	}

	// 3. Call Repository (version checking is done atomically in the DB)
	newVersion, err := a.flags.DeleteFlag(r.Context(), key, version)
	if err != nil {
		// Check for known business errors (not found, version conflict)
		if handleStoreError(w, r, err, key) {
			return
		}

		// System Error: Internal Server Error
		log.Error("failed to delete flag from db", slog.String("key", key), slog.String("error", err.Error()))
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, ErrorResponse{
			Code:    "ERR_INTERNAL",
			Message: "Failed to delete flag",
		})
		return
	}

	// 4. Async Notification
	a.notifyCacheAsync(log, key, newVersion)

	// 5. Return Success (204 No Content)
	log.Info("flag deleted successfully", slog.String("flag_key", key))
	w.WriteHeader(http.StatusNoContent)
}

// handleUpdateFlag processes the PATCH /api/v1/flags/{key} request.
//
// Responsibilities:
// 1. Extracts the {key} path parameter.
// 2. Validates the If-Match header and parses the version.
// 3. Decodes and validates the JSON payload.
// 4. Calls the Repository to update the flag with optimistic locking.
// 5. Handles version conflicts (412) and not found errors (404).
// 6. Notifies cache asynchronously to invalidate the flag.
// 7. Returns the updated flag with a 200 OK status and ETag headers.
func (a *API) handleUpdateFlag(w http.ResponseWriter, r *http.Request) {
	log := logger.FromContext(r.Context())

	// 1. Extract Path Parameter
	key := chi.URLParam(r, "key")

	// 2. Validate If-Match Header and Parse Version
	version, errResp := requireIfMatch(r)
	if errResp != nil {
		render.Status(r, http.StatusPreconditionRequired)
		render.JSON(w, r, errResp)
		return
	}

	// 3. Decode Request
	var req UpdateFlagRequest
	if err := render.DecodeJSON(r.Body, &req); err != nil {
		log.Warn("invalid json payload", slog.String("error", err.Error()))
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, ErrorResponse{
			Code:    "ERR_INVALID_JSON",
			Message: "Invalid JSON payload: " + err.Error(),
		})
		return
	}

	// 4. Validate
	if errResp := req.Validate(); errResp != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, errResp)
		return
	}

	// 5. Build update parameters
	params := &store.UpdateFlagParams{
		Key:          key,
		Version:      version,
		Name:         req.Name,
		Description:  req.Description,
		Enabled:      req.Enabled,
		DefaultValue: req.DefaultValue,
	}

	// Map Rules if provided
	if req.Rules != nil {
		var rules []ruleengine.Rule
		if len(*req.Rules) > 0 {
			// Validation already confirmed this is valid JSON
			_ = json.Unmarshal(*req.Rules, &rules)
		}
		params.Rules = &rules
	}

	// 6. Call Repository (version checking is done atomically in the DB)
	updatedFlag, err := a.flags.UpdateFlag(r.Context(), params)
	if err != nil {
		// Check for known business errors (not found, version conflict)
		if handleStoreError(w, r, err, key) {
			return
		}

		// System Error
		log.Error("failed to update flag in db", slog.String("key", key), slog.String("error", err.Error()))
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, ErrorResponse{
			Code:    "ERR_INTERNAL",
			Message: "Failed to update flag",
		})
		return
	}

	// 7. Map to Response DTO
	resp := mapStoreFlagToResponse(updatedFlag)

	// 8. Set Response Headers
	setETagHeaders(w, updatedFlag.Version)

	// 9. Async Notification
	a.notifyCacheAsync(log, key, updatedFlag.Version)

	// 10. Return Success
	log.Info("flag updated successfully", slog.String("flag_key", key), slog.Int64("new_version", updatedFlag.Version))
	render.Status(r, http.StatusOK)
	render.JSON(w, r, resp)
}

// --- Private Helpers ---

// parseOptionalInt extracts an integer from the query string.
// If the parameter is missing, it returns the defaultValue.
// It only returns an error if the parameter is present but malformed.
func parseOptionalInt(r *http.Request, key string, defaultValue int) (int, error) {
	valStr := r.URL.Query().Get(key)
	if valStr == "" {
		return defaultValue, nil
	}
	val, err := strconv.Atoi(valStr)
	if err != nil {
		return 0, fmt.Errorf("parameter '%s' must be an integer", key)
	}
	return val, nil
}

// requireIfMatch validates that the If-Match header is present and parses the version from it.
// Returns the version number and an ErrorResponse if validation fails.
// The ETag format is expected to be a quoted integer: "<version>".
func requireIfMatch(r *http.Request) (version int64, errResp *ErrorResponse) {
	ifMatch := r.Header.Get("If-Match")
	if ifMatch == "" {
		return 0, &ErrorResponse{
			Code:    "ERR_PRECONDITION_REQUIRED",
			Message: "The If-Match header is required. Please fetch the flag first to obtain an ETag.",
		}
	}

	// Parse ETag - expected format: "<version>"
	// Remove surrounding quotes
	etag := strings.Trim(ifMatch, `"`)
	version, err := strconv.ParseInt(etag, 10, 64)
	if err != nil {
		return 0, &ErrorResponse{
			Code:    "ERR_INVALID_ETAG",
			Message: "Invalid ETag format. Expected a version number.",
		}
	}

	return version, nil
}

// setETagHeaders sets the ETag and Cache-Control headers for a flag response.
func setETagHeaders(w http.ResponseWriter, version int64) {
	w.Header().Set("ETag", fmt.Sprintf(`"%d"`, version))
	w.Header().Set("Cache-Control", "no-cache")
}

// handleStoreError maps store layer errors to appropriate HTTP responses.
// Returns true if the error was handled, false if it's an unexpected error.
func handleStoreError(w http.ResponseWriter, r *http.Request, err error, key string) bool {
	if strings.Contains(err.Error(), "not found") {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, ErrorResponse{
			Code:    "ERR_NOT_FOUND",
			Message: fmt.Sprintf("Flag with key '%s' not found", key),
		})
		return true
	}

	if strings.Contains(err.Error(), "version conflict") {
		render.Status(r, http.StatusPreconditionFailed)
		render.JSON(w, r, ErrorResponse{
			Code:    "ERR_PRECONDITION_FAILED",
			Message: "The flag has been modified. Please fetch the latest version and retry.",
		})
		return true
	}

	return false
}

// mapStoreFlagToResponse converts the DB entity to the API Response DTO.
// This handles the complexity of JSONB serialization for Rules and Versioning.
func mapStoreFlagToResponse(f *store.Flag) Flag {
	// Convert []ruleengine.Rule -> json.RawMessage
	var rulesJSON json.RawMessage

	if len(f.Rules) > 0 {
		// If we have rules, marshal them properly
		b, _ := json.Marshal(f.Rules)
		rulesJSON = b
	} else {
		// If empty, explicitly return "[]" instead of "null"
		rulesJSON = json.RawMessage("[]")
	}

	return Flag{
		ID:           f.ID,
		Key:          f.Key,
		Name:         f.Name,
		Description:  f.Description,
		Enabled:      f.Enabled,
		DefaultValue: f.DefaultValue,
		Rules:        rulesJSON,
		CreatedAt:    f.CreatedAt,
		UpdatedAt:    f.UpdatedAt,
		Etag:         fmt.Sprintf("%d", f.Version),
	}
}

// notifyCacheAsync try to notify the cache asynchronously about a flag update.
func (a *API) notifyCacheAsync(log *slog.Logger, flagKey string, version int64) {
	go func(key string, ver int64) {
		// Create a context disconnected from the HTTP request.
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		const maxRetries = 3
		baseDelay := 100 * time.Millisecond

		for i := 0; i <= maxRetries; i++ {
			err := a.cache.PublishUpdate(ctx, key, ver)
			if err == nil {
				return
			}

			if i == maxRetries {
				log.Error("CRITICAL: failed to push update event after retries",
					slog.String("key", key),
					slog.Int64("version", ver),
					slog.String("error", err.Error()))
				return
			}

			// Simple exponential backoff
			log.Warn("failed to push update, retrying...",
				slog.String("key", key),
				slog.Int("attempt", i+1),
				slog.String("error", err.Error()))

			time.Sleep(baseDelay * time.Duration(1<<i))
		}
	}(flagKey, version)
}
