// Package ruleengine provides the core logic for feature flag evaluation.
// It implements a Strategy pattern where different rule types (strategies)
// are evaluated against a user context to determine a boolean result.
package ruleengine

import "encoding/json"

// Context represents the input data regarding the entity requesting the flag.
type Context struct {
	// UserID is the primary identifier for the user/entity.
	// It is required for most targeting rules (lists, percentage rollouts).
	UserID string `json:"user_id"`

	// Attributes is a flexible map for arbitrary targeting data (e.g., "region", "email").
	// This allows the engine to be forward-compatible with new rule types.
	Attributes map[string]string `json:"attributes"`
}

// EvaluationInput aggregates all the context needed to perform an evaluation.
// Using a struct allows us to add new fields in the future (e.g., CurrentTime, Environment)
// without breaking the Evaluator interface signature.
type EvaluationInput struct {
	// User holds the attributes of the entity requesting the flag (The "Who").
	User Context

	// FlagKey is the unique identifier of the flag being evaluated (The "What").
	// Used as a salt for deterministic hashing strategies (e.g., Percentage).
	FlagKey string
}

// Rule represents a single targeting rule configuration.
// This struct mirrors the JSON structure stored in the PostgreSQL 'conditions' column
// and the Redis L2 cache.
type Rule struct {
	// ID is the unique identifier of the rule (from DB).
	ID string `json:"id"`

	// Type defines the strategy to use (e.g., "USER_ID_LIST", "PERCENTAGE").
	// This discriminator tells the engine which Evaluator to invoke.
	Type string `json:"type"`

	// Value contains the specific parameters for the rule.
	// It is a RawMessage because the structure depends on the Type.
	// Examples:
	// - USER_ID_LIST: {"user_ids": ["a", "b"]}
	// - PERCENTAGE:   {"percentage": 10, "attribute": "user_id"}
	Value json.RawMessage `json:"value"`
}
