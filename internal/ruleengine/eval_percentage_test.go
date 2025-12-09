package ruleengine

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper to generate a cryptographically random string.
// Ensures our tests are not biased by sequential patterns.
func generateRandomID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)
}

// TestPercentageEvaluator_Boundaries performs Fuzz Testing on edge cases.
// It proves that 0% NEVER allows anyone and 100% ALWAYS allows everyone.
func TestPercentageEvaluator_Boundaries(t *testing.T) {
	t.Parallel()

	evaluator := PercentageEvaluator{}
	fuzzIterations := 10000

	t.Run("0% Rollout - Fuzz Test", func(t *testing.T) {
		// Arrange: Pre-compiled rule data
		ruleData := percentageRuleData{Percentage: 0, Attribute: "user_id"}

		for i := range fuzzIterations {
			input := EvaluationInput{
				User:    Context{UserID: generateRandomID()},
				FlagKey: "any-flag",
			}

			// Act
			got, err := evaluator.Eval(ruleData, input)

			// Assert
			require.NoError(t, err)
			if got {
				t.Fatalf("Failed at iteration %d: 0%% rollout returned true", i)
			}
		}
	})

	t.Run("100% Rollout - Fuzz Test", func(t *testing.T) {
		ruleData := percentageRuleData{Percentage: 100, Attribute: "user_id"}

		for i := range fuzzIterations {
			input := EvaluationInput{
				User:    Context{UserID: generateRandomID()},
				FlagKey: "any-flag",
			}

			// Act
			got, err := evaluator.Eval(ruleData, input)

			// Assert
			require.NoError(t, err)
			if !got {
				t.Fatalf("Failed at iteration %d: 100%% rollout returned false", i)
			}
		}
	})
}

// TestPercentageEvaluator_Determinism verifies Stickiness and Salt effectiveness.
func TestPercentageEvaluator_Determinism(t *testing.T) {
	t.Parallel()

	evaluator := PercentageEvaluator{}
	ruleData := percentageRuleData{Percentage: 50, Attribute: "user_id"}

	t.Run("Stickiness (Same User + Same Flag = SAME Result)", func(t *testing.T) {
		// Arrange
		fixedUser := generateRandomID()
		fixedFlag := "sticky-feature"

		input := EvaluationInput{User: Context{UserID: fixedUser}, FlagKey: fixedFlag}

		// Act 1: Baseline
		initialResult, err := evaluator.Eval(ruleData, input)
		require.NoError(t, err)

		// Act 2: Repeat 10.000 times
		for i := range 10000 {
			got, err := evaluator.Eval(ruleData, input)
			require.NoError(t, err)
			assert.Equal(t, initialResult, got, "Result flipped for same input on iteration %d", i)
		}
	})

	t.Run("Salt Effectiveness (Same User + Different Random Flags)", func(t *testing.T) {
		// Verify that the FlagKey effectively changes the hash bucket.
		// A single user should NOT get the same result across 10.000 random flags (at 50% rollout).
		fixedUser := generateRandomID()

		trueCount := 0
		falseCount := 0
		iterations := 10000 // Increased sample size for statistical safety

		for range iterations {
			input := EvaluationInput{
				User:    Context{UserID: fixedUser},
				FlagKey: generateRandomID(), // Random Flag Key (Salt)
			}
			got, _ := evaluator.Eval(ruleData, input)

			if got {
				trueCount++
			} else {
				falseCount++
			}
		}

		// Assert: Ensure distribution variance
		t.Logf("User %s across %d random flags: True=%d, False=%d", fixedUser, iterations, trueCount, falseCount)

		// It is statistically impossible for Murmur3 to yield 10.000 consecutive TRUEs or FALSEs
		// for random salts at 50% probability.
		assert.Greater(t, trueCount, 0, "User got FALSE for all random flags - hashing ignores salt")
		assert.Greater(t, falseCount, 0, "User got TRUE for all random flags - hashing ignores salt")
	})
}

// TestPercentageEvaluator_Distribution validates the hashing uniformity via Monte Carlo simulation.
func TestPercentageEvaluator_Distribution(t *testing.T) {
	t.Parallel()

	evaluator := PercentageEvaluator{}

	scenarios := []struct {
		percentage int
		tolerance  float64
	}{
		{percentage: 25, tolerance: 2.0},
		{percentage: 50, tolerance: 2.0},
		{percentage: 75, tolerance: 2.0},
	}

	sampleSize := 10000

	for _, sc := range scenarios {
		t.Run(fmt.Sprintf("Target %d%%", sc.percentage), func(t *testing.T) {
			ruleData := percentageRuleData{Percentage: sc.percentage, Attribute: "user_id"}
			trueCount := 0

			for range sampleSize {
				input := EvaluationInput{
					User:    Context{UserID: generateRandomID()},
					FlagKey: "simulation-flag",
				}

				got, err := evaluator.Eval(ruleData, input)
				require.NoError(t, err)
				if got {
					trueCount++
				}
			}

			actualPercentage := (float64(trueCount) / float64(sampleSize)) * 100.0
			t.Logf("Target: %d%%. Actual: %.2f%%", sc.percentage, actualPercentage)

			assert.InDelta(t, float64(sc.percentage), actualPercentage, sc.tolerance,
				"Hash distribution is biased")
		})
	}
}

// TestPercentageEvaluator_Validation checks basic error handling and Type Safety.
func TestPercentageEvaluator_Validation(t *testing.T) {
	tests := []struct {
		name     string
		ruleData any
		input    EvaluationInput
		want     bool
		wantErr  bool
	}{
		{
			name:     "Should fail if required attribute (user_id) is missing",
			ruleData: percentageRuleData{Percentage: 50, Attribute: "user_id"},
			input:    EvaluationInput{User: Context{UserID: ""}, FlagKey: "flag-a"},
			want:     false,
			wantErr:  false, // Mismatch
		},
		{
			name:     "Should error on invalid rule data type (not a struct)",
			ruleData: "not-a-struct", // Invalid type passed by engine
			input:    EvaluationInput{User: Context{UserID: "u-1"}, FlagKey: "f-1"},
			want:     false,
			wantErr:  true, // System Error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evaluator := PercentageEvaluator{}
			got, err := evaluator.Eval(tt.ruleData, tt.input)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}
