package config

import (
	"time"
)

// DataPlaneConfig configures the gRPC API server.
type DataPlaneConfig struct {
	Port string `envconfig:"PORT" default:"50051"`
	Host string `envconfig:"HOST" default:"0.0.0.0"`

	// gRPC specific
	MaxConcurrentStreams uint32        `envconfig:"MAX_CONCURRENT_STREAMS" default:"100"`
	KeepaliveTime        time.Duration `envconfig:"KEEPALIVE_TIME" default:"120s"`
	KeepaliveTimeout     time.Duration `envconfig:"KEEPALIVE_TIMEOUT" default:"20s"`
	MaxConnectionAge     time.Duration `envconfig:"MAX_CONNECTION_AGE" default:"300s"`
}

// Validate performs validation on the DataPlaneConfig.
func (c *DataPlaneConfig) Validate() error {
	// Validate port
	if err := validatePort(c.Port, "data plane"); err != nil {
		return err
	}

	// Validate host
	if err := validateHost(c.Host, "data plane"); err != nil {
		return err
	}

	return nil
}
