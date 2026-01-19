package config

import (
	"fmt"
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

	// L1 Cache configuration (in-memory cache layer)
	L1CacheCapacity int           `envconfig:"L1_CACHE_CAPACITY" default:"10000" validate:"min=1"`
	L1CacheTTL      time.Duration `envconfig:"L1_CACHE_TTL" default:"60s" validate:"gt=0"`
}

// Validate performs validation on the DataPlaneConfig.
func (c *DataPlaneConfig) Validate(environment string) error {
	// Validate port
	if err := validatePort(c.Port, "data plane"); err != nil {
		return err
	}

	// Validate host
	if err := validateHost(c.Host, "data plane"); err != nil {
		return err
	}

	// Production-specific minimum requirements
	if environment == EnvironmentProduction {
		if c.L1CacheCapacity < 1000 {
			return fmt.Errorf("data plane L1 cache capacity must be at least 1000 in production, got %d", c.L1CacheCapacity)
		}

		if c.L1CacheTTL < 10*time.Second {
			return fmt.Errorf("data plane L1 cache TTL must be at least 10s in production, got %v", c.L1CacheTTL)
		}
	}

	return nil
}
