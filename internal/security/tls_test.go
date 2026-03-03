package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTLSLoader(t *testing.T) {
	tests := []struct {
		name     string
		certFile string
		keyFile  string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "Should fail when cert file is empty",
			certFile: "",
			keyFile:  "/path/to/key.pem",
			wantErr:  true,
			errMsg:   "TLS cert and key file paths are required",
		},
		{
			name:     "Should fail when key file is empty",
			certFile: "/path/to/cert.pem",
			keyFile:  "",
			wantErr:  true,
			errMsg:   "TLS cert and key file paths are required",
		},
		{
			name:     "Should fail when both cert and key files are empty",
			certFile: "",
			keyFile:  "",
			wantErr:  true,
			errMsg:   "TLS cert and key file paths are required",
		},
		{
			name:     "Should succeed with valid file paths",
			certFile: "/path/to/cert.pem",
			keyFile:  "/path/to/key.pem",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader, err := NewTLSLoader(tt.certFile, tt.keyFile)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, loader)
			} else {
				require.NoError(t, err)
				require.NotNil(t, loader)
			}
		})
	}
}

func TestTLSLoader_LoadConfig(t *testing.T) {
	tests := []struct {
		name     string
		certFile string
		keyFile  string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "Should fail with nonexistent certificate file",
			certFile: "/nonexistent/cert.pem",
			keyFile:  "/nonexistent/key.pem",
			wantErr:  true,
			errMsg:   "failed to load TLS credentials",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader, err := NewTLSLoader(tt.certFile, tt.keyFile)
			require.NoError(t, err, "NewTLSLoader should succeed with valid paths")
			require.NotNil(t, loader, "loader should not be nil")

			tlsConfig, err := loader.LoadConfig()

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, tlsConfig)
			} else {
				require.NoError(t, err)
				require.NotNil(t, tlsConfig)
				assert.Len(t, tlsConfig.Certificates, 1)
			}
		})
	}
}
