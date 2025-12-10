package logger

import (
	"context"
	"log/slog"
)

// contextKey is a private type to prevent key collisions in the context map.
// Using an empty struct is zero-allocation and strictly separates our key
// from any other package's keys.
type contextKey struct{}

// WithContext returns a new context containing the provided logger.
// This is primarily used by Middleware to inject a request-scoped logger.
func WithContext(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, logger)
}

// FromContext retrieves the logger from the context.
// Design Choice: This function NEVER returns nil.
// If no logger is found in the context (e.g., inside a unit test without middleware),
// it safely falls back to the global default logger.
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(contextKey{}).(*slog.Logger); ok {
		return logger
	}
	// Safety net: Prevent nil pointer dereference in business logic
	return slog.Default()
}
