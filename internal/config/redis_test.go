package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		want    func(t *testing.T, cfg *Config)
		wantErr bool
	}{
		{
			name: "Should fail validation with PingMaxRetries < 1",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_REDIS_PING_MAX_RETRIES": "0",
			}),
			wantErr: true,
		},
		{
			name: "Should parse valid PingMaxRetries and PingBackoff",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_REDIS_PING_MAX_RETRIES": "8",
				"HEIMDALL_REDIS_PING_BACKOFF":     "3s",
			}),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 8, cfg.Redis.PingMaxRetries)
				assert.Equal(t, 3*time.Second, cfg.Redis.PingBackoff)
			},
			wantErr: false,
		},
		{
			name: "Should fail validation with invalid PingBackoff duration",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_REDIS_PING_BACKOFF": "notaduration",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation when Redis password missing in production",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				delete(cfg, "HEIMDALL_REDIS_PASSWORD")
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "Should fail validation when Redis TLS disabled in production",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				cfg["HEIMDALL_REDIS_TLS_ENABLED"] = "false"
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "Should pass validation with Redis URL in production",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				// Replace individual Redis settings with URL
				delete(cfg, "HEIMDALL_REDIS_HOST")
				delete(cfg, "HEIMDALL_REDIS_PORT")
				delete(cfg, "HEIMDALL_REDIS_PASSWORD")
				delete(cfg, "HEIMDALL_REDIS_TLS_ENABLED")
				cfg["HEIMDALL_REDIS_URL"] = "rediss://:password@redis.example.com:6379/0"
				return cfg
			}(),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "rediss://:password@redis.example.com:6379/0", cfg.Redis.URL)
				assert.True(t, cfg.Redis.IsConfigured())
			},
			wantErr: false,
		},
		{
			name: "Should fail validation when Redis MinIdleConns greater than PoolSize",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_REDIS_POOL_SIZE":      "20",
				"HEIMDALL_REDIS_MIN_IDLE_CONNS": "50",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation on invalid Redis DB number",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_REDIS_DB": "16", // Max is 15
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation on negative Redis DB number",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_REDIS_DB": "-1",
			}),
			wantErr: true,
		},
		{
			name: "Should allow passwordless Redis in development",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_APP_ENV":        "development",
				"HEIMDALL_REDIS_PASSWORD": "", // Override to remove password
			}),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "development", cfg.App.Environment)
				assert.Equal(t, "", cfg.Redis.Password)
			},
			wantErr: false,
		},
		{
			name: "Should fail validation with short Redis password in production",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				cfg["HEIMDALL_REDIS_PASSWORD"] = "short" // Less than 12 chars
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "Should fail validation with invalid Redis URL scheme",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				delete(cfg, "HEIMDALL_REDIS_HOST")
				delete(cfg, "HEIMDALL_REDIS_PORT")
				delete(cfg, "HEIMDALL_REDIS_PASSWORD")
				delete(cfg, "HEIMDALL_REDIS_TLS_ENABLED")
				cfg["HEIMDALL_REDIS_URL"] = "http://redis.example.com:6379/0" // Wrong scheme
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "Should fail validation with Redis URL having invalid DB number",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				delete(cfg, "HEIMDALL_REDIS_HOST")
				delete(cfg, "HEIMDALL_REDIS_PORT")
				delete(cfg, "HEIMDALL_REDIS_PASSWORD")
				delete(cfg, "HEIMDALL_REDIS_TLS_ENABLED")
				cfg["HEIMDALL_REDIS_URL"] = "redis://redis.example.com:6379/16" // DB 16 > 15
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "Should fail validation with Redis URL having non-numeric DB",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				delete(cfg, "HEIMDALL_REDIS_HOST")
				delete(cfg, "HEIMDALL_REDIS_PORT")
				delete(cfg, "HEIMDALL_REDIS_PASSWORD")
				delete(cfg, "HEIMDALL_REDIS_TLS_ENABLED")
				cfg["HEIMDALL_REDIS_URL"] = "redis://redis.example.com:6379/abc" // Non-numeric DB
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "Should fail validation with non-numeric port",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_REDIS_PORT": "abc",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation with host containing leading whitespace",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_REDIS_HOST": " localhost",
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
