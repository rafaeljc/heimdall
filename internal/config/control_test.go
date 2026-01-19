package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestControlPlaneConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		want    func(t *testing.T, cfg *Config)
		wantErr bool
	}{
		{
			name: "Should fail validation when TLS enabled without certificates",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_CONTROL_TLS_ENABLED": "true",
			}),
			wantErr: true,
		},
		{
			name: "Should pass validation when TLS properly configured with cert and key",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_CONTROL_TLS_ENABLED":   "true",
				"HEIMDALL_SERVER_CONTROL_TLS_CERT_FILE": "/certs/tls.crt",
				"HEIMDALL_SERVER_CONTROL_TLS_KEY_FILE":  "/certs/tls.key",
			}),
			want: func(t *testing.T, cfg *Config) {
				assert.True(t, cfg.Server.Control.TLSEnabled)
				assert.Equal(t, "/certs/tls.crt", cfg.Server.Control.TLSCert)
				assert.Equal(t, "/certs/tls.key", cfg.Server.Control.TLSKey)
			},
			wantErr: false,
		},
		{
			name: "Should fail validation when control plane API key missing in production",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				delete(cfg, "HEIMDALL_SERVER_CONTROL_API_KEY_HASH") // Remove API key to trigger validation error
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "Should fail validation when control plane TLS disabled in production",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				cfg["HEIMDALL_SERVER_CONTROL_TLS_ENABLED"] = "false" // Disable TLS
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "Should fail validation with invalid API key hash length in production",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				cfg["HEIMDALL_SERVER_CONTROL_API_KEY_HASH"] = "aaaaaa" // Not 64 chars
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "Should fail validation with non-hex API key hash in production",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				cfg["HEIMDALL_SERVER_CONTROL_API_KEY_HASH"] = "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz" // 64 chars but not hex
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "Should pass validation with exactly 12 char password in production",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				cfg["HEIMDALL_DB_PASSWORD"] = "exactly12chr"    // Exactly 12 chars
				cfg["HEIMDALL_REDIS_PASSWORD"] = "redis_pass12" // Exactly 12 chars
				return cfg
			}(),
			wantErr: false,
		},
		{
			name: "Should fail validation with port 0",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_CONTROL_PORT": "0",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation when MaxHeaderBytes is zero",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_CONTROL_MAX_HEADER_BYTES": "0",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation when MaxHeaderBytes is negative",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_CONTROL_MAX_HEADER_BYTES": "-100",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation with host containing leading whitespace",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_CONTROL_HOST": " 0.0.0.0",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation with host containing trailing whitespace",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_CONTROL_HOST": "0.0.0.0 ",
			}),
			wantErr: true,
		},
		{
			name:    "Should verify control plane timeout defaults",
			envVars: mergeEnvVars(map[string]string{}),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "8080", cfg.Server.Control.Port)
				assert.Equal(t, "0.0.0.0", cfg.Server.Control.Host)
				assert.Equal(t, 10*time.Second, cfg.Server.Control.ReadTimeout)
				assert.Equal(t, 10*time.Second, cfg.Server.Control.WriteTimeout)
				assert.Equal(t, 5*time.Second, cfg.Server.Control.ReadHeaderTimeout)
				assert.Equal(t, 60*time.Second, cfg.Server.Control.IdleTimeout)
				assert.Equal(t, 524288, cfg.Server.Control.MaxHeaderBytes) // 512KB
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
