package health

import "context"

// Checker defines the contract for any component that needs to report its health status.
// Implementations should be thread-safe and non-blocking.
type Checker interface {
	// Name returns the unique identifier of the component (e.g., "postgres", "redis").
	Name() string
	// Check performs the health verification. It returns nil if healthy, or an error if failed.
	// The provided context should be used to respect timeouts.
	Check(ctx context.Context) error
}
