package ruleengine

import (
	"encoding/json"
	"fmt"
)

const (
	// MaxUserIDListSize limits the number of user IDs in a single USER_ID_LIST rule.
	// Large user lists (>10K) indicate a design anti-pattern and would violate P99 < 20ms SLO:
	// - 10K IDs: ~3-5ms compilation time (within budget)
	// - 100K IDs: ~17-62ms compilation time (exceeds SLO by 2.5-3x)
	// For large user groups, use user attributes, segments, or percentage rollouts instead.
	MaxUserIDListSize = 10_000
)

// CompileRules processes the rules and compiles their Value (JSON) into CompiledValue (efficient data structures).
// This must be called after deserializing rules from storage (DB/Redis) before evaluation.
func CompileRules(rules []Rule) error {
	for i := range rules {
		if err := compileRule(&rules[i]); err != nil {
			return fmt.Errorf("failed to compile rule %s: %w", rules[i].ID, err)
		}
	}
	return nil
}

// compileRule compiles a single rule based on its type.
func compileRule(rule *Rule) error {
	switch rule.Type {
	case RuleTypeUserIDList:
		return compileUserIDListRule(rule)
	case RuleTypePercentage:
		return compilePercentageRule(rule)
	default:
		// Unknown rule types are silently skipped (fail-open strategy)
		return nil
	}
}

// compileUserIDListRule parses the JSON Value into a map[string]struct{} for O(1) lookup.
func compileUserIDListRule(rule *Rule) error {
	// Parse the JSON structure: {"user_ids": ["id1", "id2", ...]}
	var data struct {
		UserIDs []string `json:"user_ids"`
	}

	if err := json.Unmarshal(rule.Value, &data); err != nil {
		return fmt.Errorf("invalid USER_ID_LIST rule data: %w", err)
	}

	// Early validation for empty lists (avoid unnecessary allocation)
	if len(data.UserIDs) == 0 {
		rule.CompiledValue = make(map[string]struct{})
		return nil
	}

	// Enforce size limit to maintain P99 < 20ms SLO
	if len(data.UserIDs) > MaxUserIDListSize {
		return fmt.Errorf("USER_ID_LIST rule exceeds maximum size: %d > %d (use user segments or attributes instead)", len(data.UserIDs), MaxUserIDListSize)
	}

	// Pre-allocate map capacity
	compiled := make(map[string]struct{}, len(data.UserIDs))
	for _, id := range data.UserIDs {
		compiled[id] = struct{}{}
	}

	rule.CompiledValue = compiled
	return nil
}

// compilePercentageRule parses the JSON Value into a percentageRuleData struct.
func compilePercentageRule(rule *Rule) error {
	var data percentageRuleData

	if err := json.Unmarshal(rule.Value, &data); err != nil {
		return fmt.Errorf("invalid PERCENTAGE rule data: %w", err)
	}

	// Validate percentage range
	if data.Percentage < 0 || data.Percentage > 100 {
		return fmt.Errorf("percentage must be between 0 and 100, got %d", data.Percentage)
	}

	rule.CompiledValue = data
	return nil
}
