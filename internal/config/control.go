package config

import (
	"encoding/hex"
	"fmt"
	"time"
)

// ControlPlaneConfig configures the REST API server.
type ControlPlaneConfig struct {
	Port              string        `envconfig:"PORT" default:"8080"`
	Host              string        `envconfig:"HOST" default:"0.0.0.0"`
	ReadTimeout       time.Duration `envconfig:"READ_TIMEOUT" default:"10s"`
	WriteTimeout      time.Duration `envconfig:"WRITE_TIMEOUT" default:"10s"`
	ReadHeaderTimeout time.Duration `envconfig:"READ_HEADER_TIMEOUT" default:"5s"`
	IdleTimeout       time.Duration `envconfig:"IDLE_TIMEOUT" default:"60s"`
	MaxHeaderBytes    int           `envconfig:"MAX_HEADER_BYTES" default:"524288" validate:"min=1"` // 512KB

	// Security
	APIKeyHash string `envconfig:"API_KEY_HASH"`
	TLSEnabled bool   `envconfig:"TLS_ENABLED" default:"false"`
	TLSCert    string `envconfig:"TLS_CERT_FILE"`
	TLSKey     string `envconfig:"TLS_KEY_FILE"`
}

// Validate performs validation on the ControlPlaneConfig.
func (c *ControlPlaneConfig) Validate(environment string) error {
	// Validate port
	if err := validatePort(c.Port, "control plane"); err != nil {
		return err
	}

	// Validate host
	if err := validateHost(c.Host, "control plane"); err != nil {
		return err
	}

	// Production security requirements
	if environment == EnvironmentProduction {
		if c.APIKeyHash == "" {
			return fmt.Errorf("API key hash is required in production environment")
		}
		// Validate API key hash format (SHA-256 hex)
		if err := validateSHA256Hash(c.APIKeyHash); err != nil {
			return fmt.Errorf("invalid API key hash: %w", err)
		}
		if !c.TLSEnabled {
			return fmt.Errorf("TLS must be enabled in production environment")
		}
	}

	// Validate TLS configuration
	if c.TLSEnabled && (c.TLSCert == "" || c.TLSKey == "") {
		return fmt.Errorf("TLS enabled but cert or key file not specified")
	}

	return nil
}

// validateSHA256Hash checks if the hash is a valid SHA-256 hex string (64 hex characters)
func validateSHA256Hash(hash string) error {
	if len(hash) != 64 {
		return fmt.Errorf("SHA-256 hash must be 64 characters, got %d", len(hash))
	}
	// Check if it's valid hexadecimal
	if _, err := hex.DecodeString(hash); err != nil {
		return fmt.Errorf("hash must be valid hexadecimal: %w", err)
	}
	return nil
}
