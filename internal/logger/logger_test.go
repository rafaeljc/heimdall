package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/rafaeljc/heimdall/internal/config"
	"github.com/stretchr/testify/assert"
)

// TestNew_PanicsOnNilConfig verifies that New() panics when config is nil.
func TestNew_PanicsOnNilConfig(t *testing.T) {
	assert.PanicsWithValue(t, "logger: config cannot be nil", func() {
		New(nil)
	})
}

// TestNew_CreatesValidLogger verifies New() creates a working logger with different configs.
func TestNew_CreatesValidLogger(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.AppConfig
	}{
		{
			name: "JSON format production",
			cfg: &config.AppConfig{
				Name:        "test-service",
				Version:     "v1.0.0",
				Environment: config.EnvironmentProduction,
				LogLevel:    "info",
				LogFormat:   "json",
			},
		},
		{
			name: "Text format development",
			cfg: &config.AppConfig{
				Name:        "test-service",
				Version:     "v1.0.0",
				Environment: "development",
				LogLevel:    "debug",
				LogFormat:   "text",
			},
		},
		{
			name: "Invalid format defaults to JSON",
			cfg: &config.AppConfig{
				Name:        "test-service",
				Version:     "v1.0.0",
				Environment: config.EnvironmentProduction,
				LogLevel:    "info",
				LogFormat:   "invalid",
			},
		},
		{
			name: "Empty format defaults to JSON",
			cfg: &config.AppConfig{
				Name:        "test-service",
				Version:     "v1.0.0",
				Environment: config.EnvironmentProduction,
				LogLevel:    "info",
				LogFormat:   "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := New(tt.cfg)
			assert.NotNil(t, logger)
			assert.NotPanics(t, func() {
				logger.Debug("debug")
				logger.Info("info")
				logger.Warn("warn")
				logger.Error("error")
			})
		})
	}
}

// TestParseLevel_CriticalCases verifies log level parsing handles critical cases.
func TestParseLevel_CriticalCases(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"invalid", slog.LevelInfo}, // defaults to INFO
		{"", slog.LevelInfo},        // defaults to INFO
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, parseLevel(tt.input))
	}
}

// TestNew_DefaultsToJSONForInvalidFormat verifies that invalid or empty LogFormat defaults to JSON.
func TestNew_DefaultsToJSONForInvalidFormat(t *testing.T) {
	tests := []struct {
		name      string
		logFormat string
	}{
		{
			name:      "Invalid format",
			logFormat: "invalid",
		},
		{
			name:      "Empty format",
			logFormat: "",
		},
		{
			name:      "Random string",
			logFormat: "xml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create buffer to capture logger output
			var buf bytes.Buffer

			// Create logger with invalid format
			cfg := &config.AppConfig{
				Name:        "test-service",
				Version:     "v1.0.0",
				Environment: config.EnvironmentProduction,
				LogLevel:    "info",
				LogFormat:   tt.logFormat,
			}
			logger := NewWithWriter(cfg, &buf)

			// Log a message
			logger.Info("test message", "key", "value")

			// Get output
			output := buf.String()
			assert.NotEmpty(t, output)

			// Parse as JSON to verify it's valid JSON format
			var logEntry map[string]any
			err := json.Unmarshal([]byte(output), &logEntry)
			assert.NoError(t, err, "Output should be valid JSON")

			// Verify expected fields exist in JSON output
			assert.Equal(t, "INFO", logEntry["level"])
			assert.Equal(t, "test message", logEntry["msg"])
			assert.Equal(t, "test-service", logEntry["service"])
			assert.Equal(t, "v1.0.0", logEntry["version"])
			assert.Equal(t, config.EnvironmentProduction, logEntry["env"])
			assert.Equal(t, "value", logEntry["key"])
			assert.Contains(t, logEntry, "time")
		})
	}
}
