package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncerConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		want    func(t *testing.T, cfg *Config)
		wantErr bool
	}{
		{
			name: "Should pass validation with syncer configuration",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SYNCER_ENABLED":                  "true",
				"HEIMDALL_SYNCER_POP_TIMEOUT":              "10s",
				"HEIMDALL_SYNCER_HYDRATION_CHECK_INTERVAL": "30s",
				"HEIMDALL_SYNCER_MAX_RETRIES":              "5",
				"HEIMDALL_SYNCER_BASE_RETRY_DELAY":         "2s",
				"HEIMDALL_SYNCER_HYDRATION_CONCURRENCY":    "20",
			}),
			want: func(t *testing.T, cfg *Config) {
				assert.True(t, cfg.Syncer.Enabled)
				assert.Equal(t, 10*time.Second, cfg.Syncer.PopTimeout)
				assert.Equal(t, 30*time.Second, cfg.Syncer.HydrationCheckInterval)
				assert.Equal(t, 5, cfg.Syncer.MaxRetries)
				assert.Equal(t, 2*time.Second, cfg.Syncer.BaseRetryDelay)
				assert.Equal(t, 20, cfg.Syncer.HydrationConcurrency)
			},
			wantErr: false,
		},
		{
			name: "Should fail validation when syncer PopTimeout is zero",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SYNCER_POP_TIMEOUT": "0s",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation when syncer HydrationConcurrency is zero",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SYNCER_HYDRATION_CONCURRENCY": "0",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation when syncer HydrationConcurrency is negative",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SYNCER_HYDRATION_CONCURRENCY": "-1",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation when syncer MaxRetries is negative",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SYNCER_MAX_RETRIES": "-5",
			}),
			wantErr: true,
		},
		{
			name:    "Should verify syncer defaults",
			envVars: mergeEnvVars(map[string]string{}),
			want: func(t *testing.T, cfg *Config) {
				assert.True(t, cfg.Syncer.Enabled)
				assert.Equal(t, 5*time.Second, cfg.Syncer.PopTimeout)
				assert.Equal(t, 10*time.Second, cfg.Syncer.HydrationCheckInterval)
				assert.Equal(t, 3, cfg.Syncer.MaxRetries)
				assert.Equal(t, 1*time.Second, cfg.Syncer.BaseRetryDelay)
				assert.Equal(t, 10, cfg.Syncer.HydrationConcurrency)
			},
			wantErr: false,
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
