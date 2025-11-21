// Package controlapi implements the REST API for the Heimdall Control Plane.
// It handles HTTP routing, request decoding, validation, and response formatting.
package controlapi

import "time"

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

	// CreatedAt is the timestamp of creation in UTC.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is the timestamp of the last update in UTC.
	UpdatedAt time.Time `json:"updated_at"`
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
}

// UpdateFlagRequest defines the payload for partial updates (PATCH).
// Pointers are used to distinguish between "missing field" (do nothing)
// and "false value" (explicit update to false).
type UpdateFlagRequest struct {
	Name         *string `json:"name,omitempty"`
	Description  *string `json:"description,omitempty"`
	Enabled      *bool   `json:"enabled,omitempty"`
	DefaultValue *bool   `json:"default_value,omitempty"`
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
