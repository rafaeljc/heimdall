package logger

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContext(t *testing.T) {
	// Safe to run in parallel because we are strictly reading global state
	// or working with local instances.
	t.Parallel()

	t.Run("Should return the injected logger instance when present", func(t *testing.T) {
		// Arrange
		// We use io.Discard to avoid polluting the test output.
		// We need a specific instance to verify identity.
		expectedLogger := slog.New(slog.NewJSONHandler(io.Discard, nil))
		ctx := context.Background()

		// Act
		ctx = WithContext(ctx, expectedLogger)
		got := FromContext(ctx)

		// Assert
		// assert.Same checks for pointer equality (same memory address).
		// This proves we got exactly the object we put in.
		assert.Same(t, expectedLogger, got)
	})

	t.Run("Should return the global default logger when context is empty", func(t *testing.T) {
		// Arrange
		ctx := context.Background()

		// Capture the CURRENT global default at the start of the test.
		// We don't change it, we just want to verify fallback behavior.
		currentDefault := slog.Default()

		// Act
		got := FromContext(ctx)

		// Assert
		assert.Same(t, currentDefault, got, "Should fallback to slog.Default() to avoid nil panic")
	})
}
