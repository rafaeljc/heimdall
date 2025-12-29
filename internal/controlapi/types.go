// Package controlapi implements the REST API for the Heimdall Control Plane.
// It handles HTTP routing, request decoding, validation, and response formatting.
package controlapi

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

// flagKeyRegex ensures keys are URL-safe slugs (lowercase, numbers, hyphens).
// We compile it once at package initialization for performance.
var flagKeyRegex = regexp.MustCompile(`^[a-z0-9-]+$`)

// Flag represents the feature flag resource as stored in the database.
// It maps directly to the 'flags' table in PostgreSQL and the 'Flag' schema in OpenAPI.
type Flag struct {
	// ID is the internal surrogate key. Read-only.
	ID int64 `json:"id"`

	// Key is the natural key (slug). It must be unique and URL-safe.
	Key string `json:"key"`

	// Name is the human-readable label for the flag.
	Name string `json:"name"`

	// Description provides optional context about the flag's purpose.
	Description string `json:"description"`

	// Enabled is the master switch. If false, the flag is disabled globally.
	Enabled bool `json:"enabled"`

	// DefaultValue is the fallback return when enabled but no rule matches.
	DefaultValue bool `json:"default_value"`

	// Rules is the ordered list of targeting strategies (JSONB).
	Rules json.RawMessage `json:"rules"`

	// Version is the monotonic counter for optimistic locking.
	Version int64 `json:"version"`

	// CreatedAt is the timestamp of creation in UTC.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is the timestamp of the last update in UTC.
	UpdatedAt time.Time `json:"updated_at"`
}

// -----------------------------------------------------------------------------
// Reusable Validation Logic
// -----------------------------------------------------------------------------

// validateFlagKey enforces the format and length rules for the natural key.
// It is isolated to allow reuse in other contexts (e.g., renaming, cloning).
func validateFlagKey(key string) *ErrorResponse {
	if key == "" {
		return &ErrorResponse{
			Code:    "ERR_INVALID_INPUT",
			Message: "Key is required",
		}
	}
	if len(key) < 3 || len(key) > 255 {
		return &ErrorResponse{
			Code:    "ERR_INVALID_INPUT",
			Message: "Key must be between 3 and 255 characters",
		}
	}
	if !flagKeyRegex.MatchString(key) {
		return &ErrorResponse{
			Code:    "ERR_INVALID_INPUT",
			Message: "Key must strictly contain only lowercase letters, numbers, and hyphens (slug format)",
		}
	}
	return nil
}

// validateFlagName enforces rules for the human-readable name.
func validateFlagName(name string) *ErrorResponse {
	if name == "" {
		return &ErrorResponse{
			Code:    "ERR_INVALID_INPUT",
			Message: "Name is required",
		}
	}
	if len(name) > 255 {
		return &ErrorResponse{
			Code:    "ERR_INVALID_INPUT",
			Message: "Name must be less than 255 characters",
		}
	}
	return nil
}

// CreateFlagRequest defines the payload for creating a new flag.
// Used for JSON decoding in the POST /flags endpoint.
type CreateFlagRequest struct {
	// Key is required and immutable. Matches '^[a-z0-9-]+$'.
	Key string `json:"key"`

	// Name is required.
	Name string `json:"name"`

	// Description is optional.
	Description string `json:"description,omitempty"`

	// Enabled defaults to false if omitted (Secure by Default).
	Enabled bool `json:"enabled"`

	// DefaultValue defaults to false if omitted.
	DefaultValue bool `json:"default_value"`

	// Rules is the optional list of targeting rules for this flag.
	// Allow it to be omitted or null in the payload.
	Rules json.RawMessage `json:"rules,omitempty"`
}

// Sanitize cleans up input data by trimming whitespace and normalizing case.
// This prevents "dirty" data from entering the system logic.
func (r *CreateFlagRequest) Sanitize() {
	r.Key = strings.ToLower(strings.TrimSpace(r.Key))
	r.Name = strings.TrimSpace(r.Name)
	r.Description = strings.TrimSpace(r.Description)
}

// Validate checks if the request data adheres to business rules.
// It returns a structured *ErrorResponse if validation fails, or nil if valid.
func (r *CreateFlagRequest) Validate() *ErrorResponse {
	if err := validateFlagKey(r.Key); err != nil {
		return err
	}

	if err := validateFlagName(r.Name); err != nil {
		return err
	}

	if len(r.Rules) > 0 {
		var rules map[string]interface{}
		if err := json.Unmarshal(r.Rules, &rules); err != nil {
			return &ErrorResponse{
				Code:    "ERR_INVALID_INPUT",
				Message: "Rules must be a valid JSON array or object (empty for now)",
			}
		}
	}

	return nil
}

// UpdateFlagRequest defines the payload for partial updates (PATCH).
// Pointers are used to distinguish between "missing field" (do nothing)
// and "false value" (explicit update to false).
type UpdateFlagRequest struct {
	Name         *string          `json:"name,omitempty"`
	Description  *string          `json:"description,omitempty"`
	Enabled      *bool            `json:"enabled,omitempty"`
	DefaultValue *bool            `json:"default_value,omitempty"`
	Rules        *json.RawMessage `json:"rules,omitempty"`
}

// Validate checks if the provided fields adhere to business rules.
func (r *UpdateFlagRequest) Validate() *ErrorResponse {
	if r.Name != nil {
		if err := validateFlagName(*r.Name); err != nil {
			return err
		}
	}

	if r.Rules != nil && len(*r.Rules) > 0 {
		var rules map[string]interface{}
		if err := json.Unmarshal(*r.Rules, &rules); err != nil {
			return &ErrorResponse{
				Code:    "ERR_INVALID_INPUT",
				Message: "Rules must be a valid JSON array or object",
			}
		}
	}

	return nil
}

// PaginatedResponse is a standard wrapper for list endpoints to support offset pagination.
type PaginatedResponse struct {
	// Data holds the list of resources (e.g., []Flag).
	Data interface{} `json:"data"`

	// Meta contains pagination metadata.
	Pagination Pagination `json:"pagination"`
}

// Pagination metadata for the frontend pager.
type Pagination struct {
	TotalItems  int64 `json:"total_items"`
	TotalPages  int   `json:"total_pages"`
	CurrentPage int   `json:"current_page"`
	PageSize    int   `json:"page_size"`
}

// ErrorResponse represents a standard structured API error.
// Conforms to the project's OpenAPI error schema.
type ErrorResponse struct {
	// Code is a machine-readable error code (e.g., "ERR_INVALID_INPUT").
	Code string `json:"code"`

	// Message is a human-readable description of the error.
	Message string `json:"message"`

	// Details provides optional granular validation errors.
	Details []ErrorDetail `json:"details,omitempty"`
}

// ErrorDetail provides context about specific field validation failures.
type ErrorDetail struct {
	Field string `json:"field"`
	Issue string `json:"issue"`
}
