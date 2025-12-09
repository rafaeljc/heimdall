package ruleengine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserIDEvaluator_Eval(t *testing.T) {
	// Helper to create the efficient Set structure (O(1)) expected by the evaluator.
	// This simulates the work that the Syncer/Loader will do in the future.
	createSet := func(ids ...string) map[string]struct{} {
		m := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			m[id] = struct{}{}
		}
		return m
	}

	tests := []struct {
		name     string
		ruleData any // Pass the pre-compiled map directly
		input    EvaluationInput
		want     bool
		wantErr  bool
	}{
		// --- Happy Paths ---
		{
			name:     "Should match when UserID is explicitly in the set",
			ruleData: createSet("user-123", "user-456"),
			input:    EvaluationInput{User: Context{UserID: "user-123"}},
			want:     true,
			wantErr:  false,
		},
		{
			name:     "Should NOT match when UserID is missing from the set",
			ruleData: createSet("user-123", "user-456"),
			input:    EvaluationInput{User: Context{UserID: "user-999"}},
			want:     false,
			wantErr:  false,
		},

		// --- Edge Cases & Robustness ---
		{
			name:     "Should return false if the set is empty",
			ruleData: createSet(), // empty map
			input:    EvaluationInput{User: Context{UserID: "user-123"}},
			want:     false,
			wantErr:  false,
		},
		{
			name:     "Should be strictly case sensitive",
			ruleData: createSet("User-123"),
			input:    EvaluationInput{User: Context{UserID: "user-123"}},
			want:     false,
			wantErr:  false,
		},
		{
			name:     "Should perform exact match (whitespace matters)",
			ruleData: createSet("user-123 "), // Trailing space
			input:    EvaluationInput{User: Context{UserID: "user-123"}},
			want:     false,
			wantErr:  false,
		},
		{
			// Defensive Coding: Protect against empty context
			name:     "Should return false safely if input UserID is empty",
			ruleData: createSet("user-123"),
			input:    EvaluationInput{User: Context{UserID: ""}},
			want:     false,
			wantErr:  false,
		},

		// --- Error Scenarios ---
		{
			name:     "Should return error on invalid data type (not a map)",
			ruleData: []string{"not", "a", "map"}, // Simulating engine bug
			input:    EvaluationInput{User: Context{UserID: "user-123"}},
			want:     false,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 1. Arrange
			evaluator := UserIDEvaluator{}

			// 2. Act
			got, err := evaluator.Eval(tt.ruleData, tt.input)

			// 3. Assert
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}
