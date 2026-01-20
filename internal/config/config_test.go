package config

import (
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// minimalRequiredConfig provides database and Redis config needed for all tests
func minimalRequiredConfig() map[string]string {
	return map[string]string{
		"HEIMDALL_DB_HOST":        "localhost",
		"HEIMDALL_DB_PORT":        "5432",
		"HEIMDALL_DB_NAME":        "heimdall_test",
		"HEIMDALL_DB_USER":        "test_user",
		"HEIMDALL_DB_PASSWORD":    "test_pass",
		"HEIMDALL_REDIS_HOST":     "localhost",
		"HEIMDALL_REDIS_PORT":     "6379",
		"HEIMDALL_REDIS_PASSWORD": "redis_password_123",
	}
}

// mergeEnvVars merges additional env vars with minimal required config
func mergeEnvVars(additional map[string]string) map[string]string {
	result := minimalRequiredConfig()
	maps.Copy(result, additional)
	return result
}

// validProductionConfig returns a complete valid production configuration
// with all required database, Redis, and control plane settings for production tests
func validProductionConfig() map[string]string {
	return map[string]string{
		// App
		"HEIMDALL_APP_ENV": "production",

		// Database
		"HEIMDALL_DB_HOST":     "prod-db.example.com",
		"HEIMDALL_DB_PORT":     "5432",
		"HEIMDALL_DB_NAME":     "heimdall_prod",
		"HEIMDALL_DB_USER":     "prod_user",
		"HEIMDALL_DB_PASSWORD": "SuperSecure123!",
		"HEIMDALL_DB_SSL_MODE": "require",

		// Redis
		"HEIMDALL_REDIS_HOST":        "prod-redis.example.com",
		"HEIMDALL_REDIS_PORT":        "6379",
		"HEIMDALL_REDIS_PASSWORD":    "RedisSecure123!",
		"HEIMDALL_REDIS_TLS_ENABLED": "true",

		// Control Plane
		"HEIMDALL_SERVER_CONTROL_API_KEY_HASH":  "5dec7e1c36e8ec7f526cfa8ff6dc788daad76f6dd34467662eb47990dca6b55d",
		"HEIMDALL_SERVER_CONTROL_TLS_ENABLED":   "true",
		"HEIMDALL_SERVER_CONTROL_TLS_CERT_FILE": "/certs/control-cert.pem",
		"HEIMDALL_SERVER_CONTROL_TLS_KEY_FILE":  "/certs/control-key.pem",
	}
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		want    func(t *testing.T, cfg *Config)
		wantErr bool
	}{
		{
			name:    "Should use defaults when no env vars are set",
			envVars: minimalRequiredConfig(),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "heimdall", cfg.App.Name)
				assert.Equal(t, "dev", cfg.App.Version)
				assert.Equal(t, "development", cfg.App.Environment)
				assert.Equal(t, "info", cfg.App.LogLevel)
				assert.Equal(t, "text", cfg.App.LogFormat)
				assert.Equal(t, 30*time.Second, cfg.App.ShutdownTimeout)
				assert.Equal(t, "8080", cfg.Server.Control.Port)
				assert.Equal(t, "50051", cfg.Server.Data.Port)
			},
			wantErr: false,
		},
		{
			name: "Should load all custom environment variables correctly",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_APP_NAME":             "test-app",
				"HEIMDALL_APP_VERSION":          "1.0.0",
				"HEIMDALL_APP_ENV":              "staging",
				"HEIMDALL_APP_LOG_LEVEL":        "debug",
				"HEIMDALL_APP_LOG_FORMAT":       "json",
				"HEIMDALL_APP_SHUTDOWN_TIMEOUT": "60s",
				"HEIMDALL_SERVER_CONTROL_PORT":  "9090",
				"HEIMDALL_SERVER_DATA_PORT":     "50052",
			}),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "test-app", cfg.App.Name)
				assert.Equal(t, "1.0.0", cfg.App.Version)
				assert.Equal(t, "staging", cfg.App.Environment)
				assert.Equal(t, "debug", cfg.App.LogLevel)
				assert.Equal(t, "json", cfg.App.LogFormat)
				assert.Equal(t, 60*time.Second, cfg.App.ShutdownTimeout)
				assert.Equal(t, "9090", cfg.Server.Control.Port)
				assert.Equal(t, "50052", cfg.Server.Data.Port)
			},
			wantErr: false,
		},
		{
			name: "Should fail validation on invalid environment value",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_APP_ENV": "invalid",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation on invalid log level",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_APP_LOG_LEVEL": "trace",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation on invalid log format",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_APP_LOG_FORMAT": "xml",
			}),
			wantErr: true,
		},
		{
			name: "Should pass validation in staging environment",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_APP_ENV": "staging",
			}),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "staging", cfg.App.Environment)
			},
			wantErr: false,
		},
		{
			name: "Should allow missing passwords in non-production environments",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_APP_ENV":        "development",
				"HEIMDALL_DB_PASSWORD":    "", // Empty password OK in development
				"HEIMDALL_REDIS_PASSWORD": "", // Empty password OK in development
			}),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "development", cfg.App.Environment)
				assert.Equal(t, "", cfg.Database.Password)
				assert.Equal(t, "", cfg.Redis.Password)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup: Set environment variables for this test
			// t.Setenv automatically prevents parallel execution and cleans up after the test
			for key, value := range tt.envVars {
				t.Setenv(key, value)
			}

			// Execute
			cfg, err := Load()

			// Assert
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

func TestHealthConfigEnvValidation(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		want    func(t *testing.T, cfg *Config)
		wantErr bool
	}{
		{
			name: "Should load valid health port and timeout",
			envVars: map[string]string{
				"HEIMDALL_HEALTH_PORT":    "9090",
				"HEIMDALL_HEALTH_TIMEOUT": "2s",
			},
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 9090, cfg.Health.Port)
				assert.Equal(t, 2*time.Second, cfg.Health.Timeout)
			},
			wantErr: false,
		},
		{
			name: "Should fail validation on port too low",
			envVars: map[string]string{
				"HEIMDALL_HEALTH_PORT": "0",
			},
			wantErr: true,
		},
		{
			name: "Should fail validation on port too high",
			envVars: map[string]string{
				"HEIMDALL_HEALTH_PORT": "65536",
			},
			wantErr: true,
		},
		{
			name: "Should fail validation on timeout too short",
			envVars: map[string]string{
				"HEIMDALL_HEALTH_TIMEOUT": "999ms",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range mergeEnvVars(tt.envVars) {
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
