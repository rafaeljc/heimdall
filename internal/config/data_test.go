package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDataPlaneConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		want    func(t *testing.T, cfg *Config)
		wantErr bool
	}{
		{
			name: "Should fail validation with port above 65535",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_PORT": "65536",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation with negative port",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_PORT": "-1",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation with port 0",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_PORT": "0",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation with non-numeric port",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_PORT": "abc",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation with host containing leading whitespace",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_HOST": " 0.0.0.0",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation with host containing trailing whitespace",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_HOST": "0.0.0.0 ",
			}),
			wantErr: true,
		},
		{
			name: "Should pass validation with valid IPv4 host",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_HOST": "127.0.0.1",
				"HEIMDALL_SERVER_DATA_PORT": "50051",
			}),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "127.0.0.1", cfg.Server.Data.Host)
				assert.Equal(t, "50051", cfg.Server.Data.Port)
			},
			wantErr: false,
		},
		{
			name: "Should pass validation with valid IPv6 host",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_HOST": "::1",
				"HEIMDALL_SERVER_DATA_PORT": "50052",
			}),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "::1", cfg.Server.Data.Host)
				assert.Equal(t, "50052", cfg.Server.Data.Port)
			},
			wantErr: false,
		},
		{
			name: "Should pass validation with hostname",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_HOST": "grpc.example.com",
				"HEIMDALL_SERVER_DATA_PORT": "443",
			}),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "grpc.example.com", cfg.Server.Data.Host)
				assert.Equal(t, "443", cfg.Server.Data.Port)
			},
			wantErr: false,
		},
		{
			name:    "Should verify data plane defaults",
			envVars: mergeEnvVars(map[string]string{}),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "50051", cfg.Server.Data.Port)
				assert.Equal(t, "0.0.0.0", cfg.Server.Data.Host)
				assert.Equal(t, 20*time.Second, cfg.Server.Data.KeepaliveTime)
				assert.Equal(t, 3*time.Second, cfg.Server.Data.KeepaliveTimeout)
				assert.Equal(t, 300*time.Second, cfg.Server.Data.MaxConnectionAge)
				assert.Equal(t, 10*time.Second, cfg.Server.Data.MaxConnectionAgeGrace)
				assert.Equal(t, 120*time.Second, cfg.Server.Data.MaxConnectionIdle)
				assert.Equal(t, 262144, cfg.Server.Data.MaxRecvMsgSize) // 256KB
				assert.Equal(t, 262144, cfg.Server.Data.MaxSendMsgSize) // 256KB
				assert.Equal(t, uint32(20), cfg.Server.Data.MaxConcurrentStreams)
				assert.Equal(t, 10000, cfg.Server.Data.L1CacheCapacity)
				assert.Equal(t, 60*time.Second, cfg.Server.Data.L1CacheTTL)
			},
			wantErr: false,
		},
		{
			name: "Should pass validation with custom gRPC settings",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_KEEPALIVE_TIME":           "60s",
				"HEIMDALL_SERVER_DATA_KEEPALIVE_TIMEOUT":        "10s",
				"HEIMDALL_SERVER_DATA_MAX_CONNECTION_AGE":       "600s",
				"HEIMDALL_SERVER_DATA_MAX_CONNECTION_AGE_GRACE": "15s",
				"HEIMDALL_SERVER_DATA_MAX_CONNECTION_IDLE":      "180s",
			}),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 60*time.Second, cfg.Server.Data.KeepaliveTime)
				assert.Equal(t, 10*time.Second, cfg.Server.Data.KeepaliveTimeout)
				assert.Equal(t, 600*time.Second, cfg.Server.Data.MaxConnectionAge)
				assert.Equal(t, 15*time.Second, cfg.Server.Data.MaxConnectionAgeGrace)
				assert.Equal(t, 180*time.Second, cfg.Server.Data.MaxConnectionIdle)
			},
			wantErr: false,
		},
		// L1 Cache Configuration Tests
		{
			name: "Should pass validation with custom L1 cache capacity in development",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_L1_CACHE_CAPACITY": "5000",
			}),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 5000, cfg.Server.Data.L1CacheCapacity)
			},
			wantErr: false,
		},
		{
			name: "Should pass validation with custom L1 cache TTL in development",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_L1_CACHE_TTL": "30s",
			}),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 30*time.Second, cfg.Server.Data.L1CacheTTL)
			},
			wantErr: false,
		},
		{
			name: "Should pass validation with minimum L1 cache values in development",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_L1_CACHE_CAPACITY": "1",
				"HEIMDALL_SERVER_DATA_L1_CACHE_TTL":      "1s",
			}),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 1, cfg.Server.Data.L1CacheCapacity)
				assert.Equal(t, 1*time.Second, cfg.Server.Data.L1CacheTTL)
			},
			wantErr: false,
		},
		{
			name: "Should fail validation with zero L1 cache capacity",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_L1_CACHE_CAPACITY": "0",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation with negative L1 cache capacity",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_L1_CACHE_CAPACITY": "-100",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation with zero L1 cache TTL",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_L1_CACHE_TTL": "0",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation with negative L1 cache TTL",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_L1_CACHE_TTL": "-5s",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation with invalid L1 cache capacity format",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_L1_CACHE_CAPACITY": "abc",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation with invalid L1 cache TTL format",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_L1_CACHE_TTL": "invalid",
			}),
			wantErr: true,
		},
		// API Key Hash Tests
		{
			name: "Should fail validation when data plane API key missing in production",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				delete(cfg, "HEIMDALL_SERVER_DATA_API_KEY_HASH") // Remove API key to trigger validation error
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "Should fail validation with invalid API key hash length in production",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				cfg["HEIMDALL_SERVER_DATA_API_KEY_HASH"] = "aaaaaa" // Not 64 chars
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "Should fail validation with non-hex API key hash in production",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				cfg["HEIMDALL_SERVER_DATA_API_KEY_HASH"] = "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz" // 64 chars but not hex
				return cfg
			}(),
			wantErr: true,
		},
		// Production-specific L1 Cache Tests
		{
			name: "Should fail validation with L1 cache capacity below 1000 in production",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				cfg["HEIMDALL_SERVER_DATA_L1_CACHE_CAPACITY"] = "999"
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "Should pass validation with L1 cache capacity exactly 1000 in production",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				cfg["HEIMDALL_SERVER_DATA_L1_CACHE_CAPACITY"] = "1000"
				return cfg
			}(),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 1000, cfg.Server.Data.L1CacheCapacity)
			},
			wantErr: false,
		},
		{
			name: "Should fail validation with L1 cache TTL below 10s in production",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				cfg["HEIMDALL_SERVER_DATA_L1_CACHE_TTL"] = "9s"
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "Should pass validation with L1 cache TTL exactly 10s in production",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				cfg["HEIMDALL_SERVER_DATA_L1_CACHE_TTL"] = "10s"
				return cfg
			}(),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 10*time.Second, cfg.Server.Data.L1CacheTTL)
			},
			wantErr: false,
		},
		// TLS Configuration Tests
		{
			name: "Should fail validation when TLS enabled without certificates",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_TLS_ENABLED": "true",
			}),
			wantErr: true,
		},
		{
			name: "Should pass validation when TLS properly configured with cert and key",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_TLS_ENABLED":   "true",
				"HEIMDALL_SERVER_DATA_TLS_CERT_FILE": "/certs/server.crt",
				"HEIMDALL_SERVER_DATA_TLS_KEY_FILE":  "/certs/server.key",
			}),
			want: func(t *testing.T, cfg *Config) {
				assert.True(t, cfg.Server.Data.TLSEnabled)
				assert.Equal(t, "/certs/server.crt", cfg.Server.Data.TLSCert)
				assert.Equal(t, "/certs/server.key", cfg.Server.Data.TLSKey)
			},
			wantErr: false,
		},
		{
			name: "Should fail validation when data plane TLS disabled in production",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				cfg["HEIMDALL_SERVER_DATA_TLS_ENABLED"] = "false" // Disable TLS
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "Should pass validation when data plane TLS enabled in production",
			envVars: func() map[string]string {
				cfg := validProductionConfig()
				cfg["HEIMDALL_SERVER_DATA_TLS_ENABLED"] = "true"
				cfg["HEIMDALL_SERVER_DATA_TLS_CERT_FILE"] = "/certs/server.crt"
				cfg["HEIMDALL_SERVER_DATA_TLS_KEY_FILE"] = "/certs/server.key"
				return cfg
			}(),
			want: func(t *testing.T, cfg *Config) {
				assert.True(t, cfg.Server.Data.TLSEnabled)
				assert.Equal(t, "/certs/server.crt", cfg.Server.Data.TLSCert)
				assert.Equal(t, "/certs/server.key", cfg.Server.Data.TLSKey)
			},
			wantErr: false,
		},
		// gRPC Resource Limits Tests
		{
			name: "Should pass validation with custom gRPC resource limits",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_MAX_RECV_MSG_SIZE":       "524288",
				"HEIMDALL_SERVER_DATA_MAX_SEND_MSG_SIZE":       "1048576",
				"HEIMDALL_SERVER_DATA_MAX_CONCURRENT_STREAMS":  "50",
			}),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 524288, cfg.Server.Data.MaxRecvMsgSize)     // 512KB
				assert.Equal(t, 1048576, cfg.Server.Data.MaxSendMsgSize)    // 1MB
				assert.Equal(t, uint32(50), cfg.Server.Data.MaxConcurrentStreams)
			},
			wantErr: false,
		},
		{
			name: "Should pass validation with minimum valid gRPC resource limits",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_MAX_RECV_MSG_SIZE":       "1",
				"HEIMDALL_SERVER_DATA_MAX_SEND_MSG_SIZE":       "1",
				"HEIMDALL_SERVER_DATA_MAX_CONCURRENT_STREAMS":  "1",
			}),
			want: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 1, cfg.Server.Data.MaxRecvMsgSize)
				assert.Equal(t, 1, cfg.Server.Data.MaxSendMsgSize)
				assert.Equal(t, uint32(1), cfg.Server.Data.MaxConcurrentStreams)
			},
			wantErr: false,
		},
		{
			name: "Should fail validation with MaxRecvMsgSize of 0",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_MAX_RECV_MSG_SIZE": "0",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation with MaxSendMsgSize of 0",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_MAX_SEND_MSG_SIZE": "0",
			}),
			wantErr: true,
		},
		{
			name: "Should fail validation with MaxConcurrentStreams of 0",
			envVars: mergeEnvVars(map[string]string{
				"HEIMDALL_SERVER_DATA_MAX_CONCURRENT_STREAMS": "0",
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
