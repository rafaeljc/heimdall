package ruleengine

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileRules_UserIDList_EmptyList(t *testing.T) {
	t.Parallel()

	// Arrange
	rules := []Rule{
		{
			ID:   "rule1",
			Type: RuleTypeUserIDList,
			Value: json.RawMessage(`{
				"user_ids": []
			}`),
		},
	}

	// Act
	err := CompileRules(rules)

	// Assert
	require.NoError(t, err, "empty user list should compile successfully")

	compiled, ok := rules[0].CompiledValue.(map[string]struct{})
	require.True(t, ok, "expected map[string]struct{}, got %T", rules[0].CompiledValue)
	assert.Empty(t, compiled, "empty user list should produce empty map")
}

func TestCompileRules_UserIDList_SizeLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		size        int
		shouldError bool
		errorMsg    string
	}{
		{
			name:        fmt.Sprintf("at limit (%d)", MaxUserIDListSize),
			size:        MaxUserIDListSize,
			shouldError: false,
		},
		{
			name:        fmt.Sprintf("below limit (%d)", MaxUserIDListSize-1),
			size:        MaxUserIDListSize - 1,
			shouldError: false,
		},
		{
			name:        fmt.Sprintf("above limit (%d)", MaxUserIDListSize+1),
			size:        MaxUserIDListSize + 1,
			shouldError: true,
			errorMsg:    "exceeds maximum size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Arrange: Generate user IDs slice
			userIDs := make([]string, tt.size)
			for i := 0; i < tt.size; i++ {
				userIDs[i] = fmt.Sprintf("user_%d", i)
			}

			userIDsJSON, err := json.Marshal(userIDs)
			require.NoError(t, err, "test setup failed: could not marshal user IDs")

			valueJSON := fmt.Sprintf(`{"user_ids": %s}`, string(userIDsJSON))

			rules := []Rule{
				{
					ID:    "rule1",
					Type:  RuleTypeUserIDList,
					Value: json.RawMessage(valueJSON),
				},
			}

			// Act
			err = CompileRules(rules)

			// Assert
			if tt.shouldError {
				require.Error(t, err, "expected error for size %d", tt.size)
				assert.Contains(t, err.Error(), tt.errorMsg, "error message should mention size limit")
				assert.Contains(t, err.Error(), "rule1", "error should contain rule ID")
			} else {
				require.NoError(t, err, "expected no error for size %d", tt.size)

				// Verify compilation succeeded
				compiled, ok := rules[0].CompiledValue.(map[string]struct{})
				require.True(t, ok, "expected map[string]struct{}, got %T", rules[0].CompiledValue)
				assert.Len(t, compiled, tt.size, "map size should match input size")

				// Spot check a few entries for correctness
				assert.Contains(t, compiled, "user_0", "first user should exist")
				if tt.size > 100 {
					assert.Contains(t, compiled, "user_100", "middle user should exist")
				}
				if tt.size > 1 {
					lastKey := fmt.Sprintf("user_%d", tt.size-1)
					assert.Contains(t, compiled, lastKey, "last user should exist")
				}
			}
		})
	}
}

func TestCompileRules_UserIDList_PreAllocation(t *testing.T) {
	t.Parallel()

	// Arrange: Create 1000 user IDs to verify map is pre-allocated correctly
	const testSize = 1_000
	userIDs := make([]string, testSize)
	for i := range testSize {
		userIDs[i] = fmt.Sprintf("user_%d", i)
	}

	userIDsJSON, err := json.Marshal(userIDs)
	require.NoError(t, err, "test setup failed")

	valueJSON := fmt.Sprintf(`{"user_ids": %s}`, string(userIDsJSON))

	rules := []Rule{
		{
			ID:    "rule1",
			Type:  RuleTypeUserIDList,
			Value: json.RawMessage(valueJSON),
		},
	}

	// Act
	err = CompileRules(rules)

	// Assert
	require.NoError(t, err, "compilation should succeed")

	compiled, ok := rules[0].CompiledValue.(map[string]struct{})
	require.True(t, ok, "expected map[string]struct{}, got %T", rules[0].CompiledValue)
	assert.Len(t, compiled, testSize, "all IDs should be compiled")

	// Verify all IDs are present (correctness check)
	for i := range testSize {
		key := fmt.Sprintf("user_%d", i)
		assert.Contains(t, compiled, key, "should contain user_%d", i)
	}
}

func TestCompileRules_MultipleRules(t *testing.T) {
	t.Parallel()

	// Arrange
	rules := []Rule{
		{
			ID:   "rule1",
			Type: RuleTypeUserIDList,
			Value: json.RawMessage(`{
				"user_ids": ["user1", "user2"]
			}`),
		},
		{
			ID:   "rule2",
			Type: RuleTypePercentage,
			Value: json.RawMessage(`{
				"percentage": 50,
				"attribute": "user_id"
			}`),
		},
	}

	// Act
	err := CompileRules(rules)

	// Assert
	require.NoError(t, err, "all rules should compile successfully")

	// Verify first rule (UserIDList)
	compiled1, ok := rules[0].CompiledValue.(map[string]struct{})
	require.True(t, ok, "rule1: expected map[string]struct{}, got %T", rules[0].CompiledValue)
	assert.Len(t, compiled1, 2, "rule1: should have 2 user IDs")
	assert.Contains(t, compiled1, "user1", "rule1: should contain user1")
	assert.Contains(t, compiled1, "user2", "rule1: should contain user2")

	// Verify second rule (Percentage)
	compiled2, ok := rules[1].CompiledValue.(percentageRuleData)
	require.True(t, ok, "rule2: expected percentageRuleData, got %T", rules[1].CompiledValue)
	assert.Equal(t, 50, compiled2.Percentage, "rule2: percentage should be 50")
	assert.Equal(t, "user_id", compiled2.Attribute, "rule2: attribute should be user_id")
}

func TestCompileRules_FailFast(t *testing.T) {
	t.Parallel()

	// Arrange: Create rules where the second one will fail
	rules := []Rule{
		{
			ID:   "valid_rule",
			Type: RuleTypeUserIDList,
			Value: json.RawMessage(`{
				"user_ids": ["user1"]
			}`),
		},
		{
			ID:   "invalid_rule",
			Type: RuleTypeUserIDList,
			Value: json.RawMessage(func() string {
				// Generate MaxUserIDListSize + 1 IDs to exceed limit
				ids := make([]string, MaxUserIDListSize+1)
				for i := 0; i <= MaxUserIDListSize; i++ {
					ids[i] = fmt.Sprintf("user_%d", i)
				}
				idsJSON, _ := json.Marshal(ids)
				return fmt.Sprintf(`{"user_ids": %s}`, string(idsJSON))
			}()),
		},
		{
			ID:   "untouched_rule",
			Type: RuleTypeUserIDList,
			Value: json.RawMessage(`{
				"user_ids": ["user3"]
			}`),
		},
	}

	// Act
	err := CompileRules(rules)

	// Assert
	require.Error(t, err, "should fail on invalid rule")
	assert.Contains(t, err.Error(), "invalid_rule", "error should contain failing rule ID")
	assert.Contains(t, err.Error(), "exceeds maximum size", "error should indicate size limit violation")

	// Verify fail-fast behavior: first rule compiled, third rule not compiled
	assert.NotNil(t, rules[0].CompiledValue, "first rule should be compiled before error")
	assert.Nil(t, rules[2].CompiledValue, "third rule should not be compiled after error (fail-fast)")
}

func TestCompileRules_UnknownRuleType(t *testing.T) {
	t.Parallel()

	// Arrange
	rules := []Rule{
		{
			ID:    "unknown_rule",
			Type:  "UNKNOWN_TYPE",
			Value: json.RawMessage(`{"some": "data"}`),
		},
	}

	// Act
	err := CompileRules(rules)

	// Assert: Unknown rule types should be silently skipped (fail-open strategy)
	assert.NoError(t, err, "unknown rule type should not cause error (fail-open)")
	assert.Nil(t, rules[0].CompiledValue, "unknown rule type should not have compiled value")
}

// TestCompileRules_InvalidJSON verifies that malformed JSON is properly rejected.
func TestCompileRules_InvalidJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		ruleType  string
		value     json.RawMessage
		wantError bool
	}{
		{
			name:      "UserIDList: invalid JSON syntax",
			ruleType:  RuleTypeUserIDList,
			value:     json.RawMessage(`{"user_ids": [invalid]}`),
			wantError: true,
		},
		{
			name:      "UserIDList: missing user_ids field (graceful degradation)",
			ruleType:  RuleTypeUserIDList,
			value:     json.RawMessage(`{"wrong_field": []}`),
			wantError: false, // Defensive: unmarshal succeeds with empty array
		},
		{
			name:      "UserIDList: user_ids is not an array",
			ruleType:  RuleTypeUserIDList,
			value:     json.RawMessage(`{"user_ids": "not_an_array"}`),
			wantError: true,
		},
		{
			name:      "Percentage: invalid JSON syntax",
			ruleType:  RuleTypePercentage,
			value:     json.RawMessage(`{"percentage": invalid}`),
			wantError: true,
		},
		{
			name:      "Percentage: percentage below 0",
			ruleType:  RuleTypePercentage,
			value:     json.RawMessage(`{"percentage": -1, "attribute": "user_id"}`),
			wantError: true,
		},
		{
			name:      "Percentage: percentage above 100",
			ruleType:  RuleTypePercentage,
			value:     json.RawMessage(`{"percentage": 101, "attribute": "user_id"}`),
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Arrange
			rules := []Rule{
				{
					ID:    "test_rule",
					Type:  tt.ruleType,
					Value: tt.value,
				},
			}

			// Act
			err := CompileRules(rules)

			// Assert
			if tt.wantError {
				require.Error(t, err, "invalid JSON/data should be rejected")
				assert.Contains(t, err.Error(), "test_rule", "error should contain rule ID")
			} else {
				assert.NoError(t, err, "should handle missing fields gracefully")
			}
		})
	}
}

// TestCompileRules_DuplicateUserIDs verifies that duplicate IDs are handled (deduplicated).
func TestCompileRules_DuplicateUserIDs(t *testing.T) {
	t.Parallel()

	// Arrange: User IDs with duplicates
	rules := []Rule{
		{
			ID:   "rule_with_duplicates",
			Type: RuleTypeUserIDList,
			Value: json.RawMessage(`{
				"user_ids": ["user1", "user2", "user1", "user3", "user2"]
			}`),
		},
	}

	// Act
	err := CompileRules(rules)

	// Assert
	require.NoError(t, err, "duplicates should not cause error")

	compiled, ok := rules[0].CompiledValue.(map[string]struct{})
	require.True(t, ok, "expected map[string]struct{}")

	// Map automatically deduplicates - should only have 3 unique entries
	assert.Len(t, compiled, 3, "duplicates should be deduplicated")
	assert.Contains(t, compiled, "user1")
	assert.Contains(t, compiled, "user2")
	assert.Contains(t, compiled, "user3")
}

// TestCompileRules_EdgeCases tests edge cases for robustness.
func TestCompileRules_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		rules     []Rule
		wantError bool
		validate  func(t *testing.T, rules []Rule)
	}{
		{
			name:      "empty rules slice",
			rules:     []Rule{},
			wantError: false,
			validate: func(t *testing.T, rules []Rule) {
				assert.Empty(t, rules, "empty slice should remain empty")
			},
		},
		{
			name: "nil rule value",
			rules: []Rule{
				{
					ID:    "nil_value",
					Type:  RuleTypeUserIDList,
					Value: nil,
				},
			},
			wantError: true,
			validate:  nil,
		},
		{
			name: "empty JSON object",
			rules: []Rule{
				{
					ID:    "empty_json",
					Type:  RuleTypeUserIDList,
					Value: json.RawMessage(`{}`),
				},
			},
			wantError: false,
			validate: func(t *testing.T, rules []Rule) {
				compiled, ok := rules[0].CompiledValue.(map[string]struct{})
				require.True(t, ok, "should compile to empty map")
				assert.Empty(t, compiled, "empty user_ids should produce empty map")
			},
		},
		{
			name: "whitespace in user IDs (exact match required)",
			rules: []Rule{
				{
					ID:   "whitespace_test",
					Type: RuleTypeUserIDList,
					Value: json.RawMessage(`{
						"user_ids": [" user1", "user2 ", " user3 "]
					}`),
				},
			},
			wantError: false,
			validate: func(t *testing.T, rules []Rule) {
				compiled := rules[0].CompiledValue.(map[string]struct{})
				assert.Contains(t, compiled, " user1", "should preserve leading whitespace")
				assert.Contains(t, compiled, "user2 ", "should preserve trailing whitespace")
				assert.Contains(t, compiled, " user3 ", "should preserve surrounding whitespace")
				assert.NotContains(t, compiled, "user1", "should NOT trim whitespace")
			},
		},
		{
			name: "unicode user IDs",
			rules: []Rule{
				{
					ID:   "unicode_test",
					Type: RuleTypeUserIDList,
					Value: json.RawMessage(`{
						"user_ids": ["用户123", "пользователь456", "🚀user789"]
					}`),
				},
			},
			wantError: false,
			validate: func(t *testing.T, rules []Rule) {
				compiled := rules[0].CompiledValue.(map[string]struct{})
				assert.Len(t, compiled, 3, "should handle unicode correctly")
				assert.Contains(t, compiled, "用户123")
				assert.Contains(t, compiled, "пользователь456")
				assert.Contains(t, compiled, "🚀user789")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Act
			err := CompileRules(tt.rules)

			// Assert
			if tt.wantError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, tt.rules)
				}
			}
		})
	}
}
