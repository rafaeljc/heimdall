package ruleengine

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEngine_Evaluate(t *testing.T) {
	// Setup helper
	createSet := func(ids ...string) map[string]struct{} {
		m := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			m[id] = struct{}{}
		}
		return m
	}

	tests := []struct {
		name       string
		rules      []Rule
		input      EvaluationInput
		want       bool
		wantLogMsg string
	}{
		// --- Happy Paths ---
		{
			name:  "Should return false if rule list is empty (Default)",
			rules: []Rule{},
			input: EvaluationInput{User: Context{UserID: "any"}, FlagKey: "test-flag"},
			want:  false,
		},
		{
			name: "Should return false if user does not match any rule (Fallthrough)",
			rules: []Rule{
				{
					Type:          RuleTypeUserIDList,
					CompiledValue: createSet("user-vip"),
				},
				{
					Type:          RuleTypePercentage,
					CompiledValue: percentageRuleData{Percentage: 0, Attribute: "user_id"},
				},
			},
			input: EvaluationInput{User: Context{UserID: "normal-user"}, FlagKey: "test-flag"},
			want:  false,
		},
		{
			name: "Should return true on first match (Priority 1 wins)",
			rules: []Rule{
				{
					ID:            "rule-vip",
					Type:          RuleTypeUserIDList,
					CompiledValue: createSet("user-vip"),
				},
				// This second rule would return false, but shouldn't be reached
				{
					ID:            "rule-percentage",
					Type:          RuleTypePercentage,
					CompiledValue: percentageRuleData{Percentage: 0},
				},
			},
			input: EvaluationInput{User: Context{UserID: "user-vip"}, FlagKey: "test-flag"},
			want:  true,
		},

		// Error Case
		{
			name: "Should fail open (skip) and LOG WARNING on unknown rule type",
			rules: []Rule{
				{
					ID:   "rule-unknown",
					Type: "GEO_LOCATION",
				},
				{
					Type:          RuleTypeUserIDList,
					CompiledValue: createSet("user-a"),
				},
			},
			input:      EvaluationInput{User: Context{UserID: "user-a"}, FlagKey: "test-flag"},
			want:       true, // Matched second rule
			wantLogMsg: "skipping unknown rule type",
		},
		{
			name: "Should fail open (skip) and LOG ERROR on strategy failure",
			rules: []Rule{
				{
					ID:            "rule-corrupted",
					Type:          RuleTypeUserIDList,
					CompiledValue: "invalid-data", // Error!
				},
				{
					Type:          RuleTypePercentage,
					CompiledValue: percentageRuleData{Percentage: 100, Attribute: "user_id"},
				},
			},
			input:      EvaluationInput{User: Context{UserID: "user-b"}, FlagKey: "test-flag"},
			want:       true,
			wantLogMsg: "rule evaluation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// 1. Thread-Safe Log Capture
			var logBuffer bytes.Buffer
			localLogger := slog.New(slog.NewTextHandler(&logBuffer, nil))

			// 2. Dependency Injection
			engine := New(localLogger)

			// 3. Act
			got := engine.Evaluate(tt.rules, tt.input)

			// 4. Assert
			assert.Equal(t, tt.want, got)
			if tt.wantLogMsg != "" {
				assert.Contains(t, logBuffer.String(), tt.wantLogMsg)
			}
		})
	}
}
