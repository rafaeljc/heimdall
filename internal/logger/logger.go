// Package logger provides a configured structured logger for the application.
// It wraps the standard library "log/slog" package to ensure consistent formatting
// (JSON in production, Text in development) and level management across services.
package logger

import (
	"io"
	"log/slog"
	"os"

	"github.com/rafaeljc/heimdall/internal/config"
)

// New creates and returns a new *slog.Logger instance based on the provided config.
// It implements the Factory Pattern, encapsulating handler creation and attribute injection.
// Output is written to os.Stdout.
func New(cfg *config.AppConfig) *slog.Logger {
	return NewWithWriter(cfg, os.Stdout)
}

// NewWithWriter creates and returns a new *slog.Logger instance based on the provided config,
// writing output to the specified io.Writer. This is useful for testing or custom output destinations.
func NewWithWriter(cfg *config.AppConfig, w io.Writer) *slog.Logger {
	if cfg == nil {
		panic("logger: config cannot be nil")
	}

	var handler slog.Handler

	opts := &slog.HandlerOptions{
		Level: parseLevel(cfg.LogLevel),
		// AddSource adds the file:line to the log (useful for debugging, expensive in prod)
		AddSource: cfg.Environment != config.EnvironmentProduction,
	}

	// Choose handler based on log format from config
	switch cfg.LogFormat {
	case "text":
		// TextHandler is human-readable: "time=... level=INFO msg=..."
		handler = slog.NewTextHandler(w, opts)
	case "json":
		// JSONHandler is machine-readable: {"time":"...","level":"INFO",...}
		handler = slog.NewJSONHandler(w, opts)
	default:
		// Default to JSON for safety
		handler = slog.NewJSONHandler(w, opts)
	}

	// Create the logger instance
	logger := slog.New(handler)

	// Inject global attributes (Identity & Metadata)
	// These will appear in every log line emitted by this logger instance or its children.
	logger = logger.With(
		slog.String("service", cfg.Name),
		slog.String("version", cfg.Version),
		slog.String("env", cfg.Environment),
	)

	return logger
}

// parseLevel converts a string to slog.Level. Defaults to INFO.
func parseLevel(s string) slog.Level {
	var level slog.Level
	// UnmarshalText handles case insensitivity (INFO, info, Info)
	if err := level.UnmarshalText([]byte(s)); err != nil {
		return slog.LevelInfo // Default safe value
	}
	return level
}
