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

	"github.com/go-chi/render"
	"github.com/rafaeljc/heimdall/internal/logger"
	"github.com/rafaeljc/heimdall/internal/ruleengine"
	"github.com/rafaeljc/heimdall/internal/store"
)

// handleCreateFlag processes the POST /api/v1/flags request.
//
// Responsibilities:
// 1. Decodes the JSON payload into the CreateFlagRequest DTO.
// 2. Sanitizes and Validates the input using the DTO's business logic.
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

	// 2. Sanitize & Validate
	// We delegate this logic to the DTO to keep the handler clean and testable.
	// Sanitize modifies the struct in-place (trimming spaces, lowercasing keys).
	req.Sanitize()

	// Validate checks business rules (length, format, required fields).
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
	a.notifyCacheAsync(log, flag.Key)

	// 7. Return Success
	log.Info("flag created successfully", slog.String("flag_key", flag.Key), slog.Int64("flag_id", flag.ID))
	render.Status(r, http.StatusCreated)
	render.JSON(w, r, resp)
}

// handleListFlags processes the GET /api/v1/flags request.
//
// Responsibilities:
// 1. Parses and sanitizes pagination parameters (page, page_size).
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

	// 2. Sanitize & Clamp (Logic Validation)
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
		Version:      f.Version,
		CreatedAt:    f.CreatedAt,
		UpdatedAt:    f.UpdatedAt,
	}
}

// notifyCacheAsync try to notify the cache asynchronously about a flag update.
func (a *API) notifyCacheAsync(log *slog.Logger, flagKey string) {
	go func(key string) {
		// Create a context disconnected from the HTTP request.
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		const maxRetries = 3
		baseDelay := 100 * time.Millisecond

		for i := 0; i <= maxRetries; i++ {
			err := a.cache.PublishUpdate(ctx, key)
			if err == nil {
				return
			}

			if i == maxRetries {
				log.Error("CRITICAL: failed to push update event after retries",
					slog.String("key", key),
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
	}(flagKey)
}
