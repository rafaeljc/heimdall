package observability

import "context"

// Checker defines the contract for any component that needs to report its health status.
// Implementations must be thread-safe and non-blocking (respecting the context).
type Checker interface {
	// Name returns the unique identifier of the component (e.g., "postgres", "redis").
	Name() string
	// Check performs the health verification. Returns nil if healthy, or an error if it fails.
	// The provided context must be used to respect timeouts.
	Check(ctx context.Context) error
}
