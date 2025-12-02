package logger

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewConfig(t *testing.T) {
	// Define the input arguments structure
	type args struct {
		serviceName string
		envStr      string
		levelStr    string
	}

	// Table-Driven Tests configuration
	tests := []struct {
		// name describes the specific behavior being tested.
		// It appears in the test output if a failure occurs.
		name string
		args args
		want Config
	}{
		{
			name: "Should default to Production/Info when inputs are empty",
			args: args{
				serviceName: "test-svc",
				envStr:      "",
				levelStr:    "",
			},
			want: Config{
				ServiceName: "test-svc",
				// We default to Prod for safety (Secure by Default)
				Environment: EnvProd,
				Level:       slog.LevelInfo,
			},
		},
		{
			name: "Should correctly parse explicit Development and Debug configuration",
			args: args{
				serviceName: "api",
				envStr:      "dev",
				levelStr:    "DEBUG",
			},
			want: Config{
				ServiceName: "api",
				Environment: EnvDev,
				Level:       slog.LevelDebug,
			},
		},
		{
			name: "Should be robust against mixed case and whitespace padding",
			args: args{
				serviceName: "api",
				envStr:      "  DEV  ", // Dirty input
				levelStr:    "info",    // Lowercase input
			},
			want: Config{
				ServiceName: "api",
				Environment: EnvDev,
				Level:       slog.LevelInfo,
			},
		},
		{
			name: "Should fallback to safe defaults (Prod) when invalid values are provided",
			args: args{
				serviceName: "api",
				envStr:      "staging",        // Unknown environment
				levelStr:    "super-critical", // Unknown level
			},
			want: Config{
				ServiceName: "api",
				Environment: EnvProd,        // Fallback
				Level:       slog.LevelInfo, // Fallback
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			got := NewConfig(tt.args.serviceName, tt.args.envStr, tt.args.levelStr)

			// Assert
			assert.Equal(t, tt.want, got)
		})
	}
}
