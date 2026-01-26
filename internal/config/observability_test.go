package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestObservabilityConfigEnvValidation(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		want    func(t *testing.T, cfg *Config)
		wantErr bool
	}{
		{
			name: "Should load valid observability port and timeout",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_OBSERVABILITY_PORT":    "9090",
				"HEIMDALL_OBSERVABILITY_TIMEOUT": "1s",
			}),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "9090", cfg.Observability.Port)
				assert.Equal(t, 1*time.Second, cfg.Observability.Timeout)
			},
			wantErr: false,
		},
		{
			name: "Should fail validation on port too low",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_OBSERVABILITY_PORT": "0",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation on port too high",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_OBSERVABILITY_PORT": "65536",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation on timeout too short",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_OBSERVABILITY_TIMEOUT": "999ms",
			}),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range tt.envVars {
				t.Setenv(key, value)
			}
			cfg, err := Load()
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.want != nil {
				tt.want(t, cfg)
			}
		})
	}
}
