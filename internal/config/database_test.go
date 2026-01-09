package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDatabaseConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		want    func(t *testing.T, cfg *Config)
		wantErr bool
	}{
		{
			name: "Should fail validation when database password missing in production",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				delete(cfg, "HEIMDALL_DB_PASSWORD")
				return cfg
			}(),
			want: func(t *testing.T, cfg *Config) {
				// Should not reach here
			},
			wantErr: true,
		},
		{
			name:    "Should pass validation when database password provided in production",
			envVars: validProductionConfig(),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "production", cfg.App.Environment)
				assert.Equal(t, "prod-db.example.com", cfg.Database.Host)
				assert.Equal(t, "SuperSecure123!", cfg.Database.Password)
				assert.Equal(t, "require", cfg.Database.SSLMode)
			},
			wantErr: false,
		},
		{
			name: "Should fail validation when database SSL mode is insecure in production",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				cfg["HEIMDALL_DB_SSL_MODE"] = "disable"
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "Should pass validation with database URL in production",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				// Replace individual DB settings with URL
				delete(cfg, "HEIMDALL_DB_HOST")
				delete(cfg, "HEIMDALL_DB_PORT")
				delete(cfg, "HEIMDALL_DB_NAME")
				delete(cfg, "HEIMDALL_DB_USER")
				delete(cfg, "HEIMDALL_DB_PASSWORD")
				delete(cfg, "HEIMDALL_DB_SSL_MODE")
				cfg["HEIMDALL_DB_URL"] = "postgres://user:pass@host:5432/db?sslmode=require"
				return cfg
			}(),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "postgres://user:pass@host:5432/db?sslmode=require", cfg.Database.URL)
				assert.True(t, cfg.Database.IsConfigured())
			},
			wantErr: false,
		},
		{
			name: "Should fail validation when database MinConns greater than MaxConns",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_DB_MIN_CONNS": "30",
				"HEIMDALL_DB_MAX_CONNS": "10",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation on invalid database SSL mode",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_DB_SSL_MODE": "invalid",
			}),
			wantErr: true,
		},
		{
			name: "Should allow passwordless database in development",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_APP_ENV":     "development",
				"HEIMDALL_DB_PASSWORD": "", // Override to remove password
			}),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "development", cfg.App.Environment)
				assert.Equal(t, "", cfg.Database.Password)
			},
			wantErr: false,
		},
		{
			name: "Should fail validation with short database password in production",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				cfg["HEIMDALL_DB_PASSWORD"] = "short" // Less than 12 chars
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "Should fail validation with database name exceeding 63 characters",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_DB_NAME": "this_is_a_very_long_database_name_that_exceeds_the_postgresql_limit_of_sixtythree_characters",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation with empty database name",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_DB_NAME": "",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation with empty database user",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_DB_USER": "",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation with invalid Postgres URL scheme",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				// Replace all DB settings with a URL (only URL is needed)
				delete(cfg, "HEIMDALL_DB_HOST")
				delete(cfg, "HEIMDALL_DB_PORT")
				delete(cfg, "HEIMDALL_DB_NAME")
				delete(cfg, "HEIMDALL_DB_USER")
				delete(cfg, "HEIMDALL_DB_PASSWORD")
				delete(cfg, "HEIMDALL_DB_SSL_MODE")
				cfg["HEIMDALL_DB_URL"] = "mysql://user:pass@host:3306/db" // Wrong scheme
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "Should fail validation with Postgres URL missing user",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				delete(cfg, "HEIMDALL_DB_HOST")
				delete(cfg, "HEIMDALL_DB_PORT")
				delete(cfg, "HEIMDALL_DB_NAME")
				delete(cfg, "HEIMDALL_DB_USER")
				delete(cfg, "HEIMDALL_DB_PASSWORD")
				delete(cfg, "HEIMDALL_DB_SSL_MODE")
				cfg["HEIMDALL_DB_URL"] = "postgres://host:5432/db" // No user
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "Should fail validation with Postgres URL missing database name",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				delete(cfg, "HEIMDALL_DB_HOST")
				delete(cfg, "HEIMDALL_DB_PORT")
				delete(cfg, "HEIMDALL_DB_NAME")
				delete(cfg, "HEIMDALL_DB_USER")
				delete(cfg, "HEIMDALL_DB_PASSWORD")
				delete(cfg, "HEIMDALL_DB_SSL_MODE")
				cfg["HEIMDALL_DB_URL"] = "postgres://user:pass@host:5432/" // No db name
				return cfg
			}(),
			wantErr: true,
		},
		{
			name:    "Should verify SSL mode defaults to prefer",
			envVars: mergeEnvVars(map[string]string{}),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "prefer", cfg.Database.SSLMode)
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
