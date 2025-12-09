// TODO(scale): Current implementation supports 1% granularity (0-100 buckets).
// For high-traffic systems requiring canary releases (e.g., 0.1%),
// we should refactor this to use Basis Points (0-10000 buckets).
package ruleengine

import (
	"fmt"

	"github.com/spaolacci/murmur3"
)

// PercentageEvaluator implements the Evaluator interface for gradual rollouts.
// It uses consistent hashing (Murmur3) to ensure the same user always falls
// into the same bucket for a specific flag (Stickiness).
type PercentageEvaluator struct{}

// percentageRuleData defines the internal schema for the rule's compiled data.
// This struct is shared within the package (so tests can access it).
type percentageRuleData struct {
	// Percentage is an integer from 0 to 100.
	Percentage int `json:"percentage"`

	// Attribute is the name of the context field to use for hashing (default: "user_id").
	Attribute string `json:"attribute"`
}

// Eval calculates the hash bucket for the user and checks if it falls within the percentage.
//
// Parameters:
//   - ruleData: Expected to be of type percentageRuleData (compiled struct).
//   - input: The evaluation input containing Context and FlagKey (Salt).
//
// Thread-Safety: This method is stateless and creates a new Hasher per call.
func (e *PercentageEvaluator) Eval(ruleData any, input EvaluationInput) (bool, error) {
	// 1. Type Assertion
	// We expect the loader/syncer to have pre-compiled the JSON into this struct.
	data, ok := ruleData.(percentageRuleData)
	if !ok {
		return false, fmt.Errorf("invalid rule data type: expected percentageRuleData, got %T", ruleData)
	}

	// 2. Resolve Hash Subject
	// Determine which attribute to hash (User ID, Email, Session ID, etc).
	// Defaults to "user_id" for backward compatibility or empty configuration.
	subjectValue := input.User.UserID

	if data.Attribute != "" && data.Attribute != "user_id" {
		// Attempt to fetch from the flexible attributes map
		if val, exists := input.User.Attributes[data.Attribute]; exists {
			subjectValue = val
		} else {
			// Fail Closed: If the required attribute is missing, we cannot hash securely.
			// Treat as a mismatch rather than an error to avoid log spam.
			return false, nil
		}
	}

	// Defensive check: Empty strings cannot be hashed reliably for distribution
	if subjectValue == "" {
		return false, nil
	}

	// 3. Construct the Composite Hash Key (Subject + Salt)
	// Format: "user-123:new-checkout-feature"
	// The FlagKey (Salt) ensures that a user who is in the "lucky 10%" for Flag A
	// is not necessarily in the "lucky 10%" for Flag B. This ensures statistical independence.
	hashKey := fmt.Sprintf("%s:%s", subjectValue, input.FlagKey)

	// 4. Calculate Hash (Murmur3)
	// We use Murmur3 (32-bit) because it provides excellent distribution (avalanche effect)
	// and is extremely fast compared to cryptographic hashes like SHA-256.
	hasher := murmur3.New32()
	_, _ = hasher.Write([]byte(hashKey)) // Write never returns error in this implementation
	hashValue := hasher.Sum32()

	// 5. Normalize to Bucket (0-99)
	// Modulo 100 maps the 32-bit integer to a 0-99 range.
	bucket := int(hashValue % 100)

	// 6. Decision
	// If percentage is 10, buckets 0 to 9 return true.
	// 10 < 10 is false.
	return bucket < data.Percentage, nil
}
