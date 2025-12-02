// Package logger provides a configured structured logger for the application.
// It wraps the standard library "log/slog" package to ensure consistent formatting
// (JSON in production, Text in development) and level management across services.
package logger

import (
	"log/slog"
	"os"
	"strings"
)

// Environment defines the runtime environment type.
type Environment string

const (
	EnvDev  Environment = "dev"
	EnvProd Environment = "prod"
)

// Config holds the configuration for the logger.
type Config struct {
	ServiceName string
	Level       slog.Level
	Environment Environment
}

// NewConfig creates a validated Config struct from raw strings.
// It handles parsing, normalization, and default values.
func NewConfig(serviceName, envStr, levelStr string) Config {
	return Config{
		ServiceName: serviceName,
		Environment: parseEnv(envStr),
		Level:       parseLevel(levelStr),
	}
}

// Setup initializes the global logger based on the provided config.
// It should be called once at the start of the application (main).
func Setup(cfg Config) {
	var handler slog.Handler

	opts := &slog.HandlerOptions{
		Level: cfg.Level,
		// AddSource adds the file:line to the log (useful for debugging, expensive in prod)
		AddSource: cfg.Environment == EnvDev,
	}

	// Choose handler based on environment
	switch cfg.Environment {
	case EnvDev:
		// TextHandler is human-readable: "time=... level=INFO msg=..."
		handler = slog.NewTextHandler(os.Stdout, opts)
	case EnvProd:
		// JSONHandler is machine-readable: {"time":"...","level":"INFO",...}
		fallthrough
	default:
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	// Set the global default logger
	logger := slog.New(handler)

	logger = logger.With(
		slog.String("service", cfg.ServiceName),
		slog.String("env", string(cfg.Environment)),
	)
	slog.SetDefault(logger)
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

// parseEnv converts a string to our internal Environment type. Defaults to Prod.
func parseEnv(s string) Environment {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "dev", "development":
		return EnvDev
	default:
		return EnvProd // Default safe value
	}
}
