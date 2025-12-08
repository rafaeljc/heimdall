package ruleengine

import (
	"fmt"
)

// UserIDEvaluator implements the Evaluator interface for allow-list strategies.
// It determines if the current user is explicitly included in a specific list of IDs.
type UserIDEvaluator struct{}

// Eval checks if the input UserID exists in the pre-compiled set.
//
// Parameters:
//   - ruleData: Expected to be of type map[string]struct{} for O(1) lookup.
//   - input: The evaluation input containing the User Context.
//
// Returns true if the UserID is found in the set.
func (e *UserIDEvaluator) Eval(ruleData any, input EvaluationInput) (bool, error) {
	// 1. Defensive Check (Fail Fast)
	// If the context has no UserID, it is impossible to match an allow-list.
	if input.User.UserID == "" {
		return false, nil
	}

	// 2. Type Assertion
	// The engine is responsible for parsing JSON into this efficient map structure.
	// If the wrong type is passed, it's an internal system error.
	allowedIDs, ok := ruleData.(map[string]struct{})
	if !ok {
		return false, fmt.Errorf("invalid rule data type: expected map[string]struct{}, got %T", ruleData)
	}

	// 3. Evaluate
	// Efficiently check for existence in the map.
	_, found := allowedIDs[input.User.UserID]

	return found, nil
}
