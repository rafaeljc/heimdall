package ruleengine

// Evaluator is the interface that all rule strategies must implement.
// It encapsulates the specific logic to determine if a user context matches a rule.
type Evaluator interface {
	// Eval checks if the input satisfies the rule's conditions.
	//
	// Parameters:
	// - ruleData: The pre-processed rule configuration (e.g., map[string]struct{}).
	//   We use 'any' to decouple the evaluation logic from the storage format (JSON).
	//   The Engine/Loader is responsible for parsing the JSON into this structure.
	// - input: The aggregated evaluation context (User, System Metadata).
	//
	// Returns:
	// - bool: True if the rule matches (user should see the feature).
	// - error: Non-nil if the ruleData type is invalid.
	Eval(ruleData any, input EvaluationInput) (bool, error)
}
