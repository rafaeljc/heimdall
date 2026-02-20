// Package security provides security-related utilities for Heimdall services.
// This includes TLS configuration, certificate loading, and credential management.
package security

import (
	"crypto/tls"
	"fmt"
)

// TLSLoader handles loading and configuring TLS certificates for secure communication.
// It encapsulates certificate file paths and provides methods to load and configure
// TLS settings according to best practices.
type TLSLoader struct {
	// CertFile is the path to the TLS certificate file (PEM format)
	certFile string

	// KeyFile is the path to the TLS private key file (PEM format)
	keyFile string
}

// NewTLSLoader creates a new TLSLoader with validation.
//
// Parameters:
//   - certFile: Path to the TLS certificate file (PEM format)
//   - keyFile: Path to the TLS private key file (PEM format)
//
// Returns:
//   - *TLSLoader: Validated loader instance
//   - error: If cert or key file paths are empty
//
// Example:
//
//	loader, err := security.NewTLSLoader(
//		"/etc/certs/server.crt",
//		"/etc/certs/server.key",
//	)
//	if err != nil {
//		log.Fatal(err)
//	}
//	tlsConfig, err := loader.LoadConfig()
func NewTLSLoader(certFile, keyFile string) (*TLSLoader, error) {
	if certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("security: TLS cert and key file paths are required")
	}

	return &TLSLoader{
		certFile: certFile,
		keyFile:  keyFile,
	}, nil
}

// LoadConfig loads the certificate and key files, returning a fully configured tls.Config.
//
// This function:
// - Loads the X.509 certificate and private key from disk
// - Returns a tls.Config with production-grade settings
// - Enforces minimum TLS 1.2 for security
//
// Returns:
//   - *tls.Config: Configured TLS settings ready for use
//   - error: If cert/key files are invalid or cannot be read
//
// Example:
//
//	loader, _ := security.NewTLSLoader(certFile, keyFile)
//	tlsConfig, err := loader.LoadConfig()
//	if err != nil {
//		log.Fatal(err)
//	}
func (l *TLSLoader) LoadConfig() (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(l.certFile, l.keyFile)
	if err != nil {
		return nil, fmt.Errorf("security: failed to load TLS credentials from %s and %s: %w",
			l.certFile, l.keyFile, err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12, // Production best practice: TLS 1.2+
	}, nil
}
