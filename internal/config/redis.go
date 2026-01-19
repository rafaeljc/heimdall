package config

import (
	"fmt"
	"strings"
	"time"
)

// RedisConfig contains Redis connection and pool settings.
type RedisConfig struct {
	// Connection can be specified as a URL or individual components
	URL      string `envconfig:"URL"` // Full connection URL
	Host     string `envconfig:"HOST"`
	Port     string `envconfig:"PORT"`
	Password string `envconfig:"PASSWORD"`
	DB       int    `envconfig:"DB" default:"0" validate:"min=0,max=15"`

	// TLS
	TLSEnabled bool `envconfig:"TLS_ENABLED" default:"false"`

	// Connection Pool
	PoolSize        int           `envconfig:"POOL_SIZE" default:"50" validate:"min=1"`
	MinIdleConns    int           `envconfig:"MIN_IDLE_CONNS" default:"10" validate:"min=0"`
	DialTimeout     time.Duration `envconfig:"DIAL_TIMEOUT" default:"5s"`
	ReadTimeout     time.Duration `envconfig:"READ_TIMEOUT" default:"10s"`
	WriteTimeout    time.Duration `envconfig:"WRITE_TIMEOUT" default:"3s"`
	PoolTimeout     time.Duration `envconfig:"POOL_TIMEOUT" default:"4s"`
	MaxRetries      int           `envconfig:"MAX_RETRIES" default:"3" validate:"min=0"`
	MinRetryBackoff time.Duration `envconfig:"MIN_RETRY_BACKOFF" default:"8ms"`
	MaxRetryBackoff time.Duration `envconfig:"MAX_RETRY_BACKOFF" default:"512ms"`

	// Ping/connection retry settings
	PingMaxRetries int           `envconfig:"PING_MAX_RETRIES" default:"5" validate:"min=1"`
	PingBackoff    time.Duration `envconfig:"PING_BACKOFF" default:"2s"`
}

// Address returns the Redis address in host:port format.
// If URL is provided, it returns that for the redis client to parse.
func (c *RedisConfig) Address() string {
	// If a full URL is provided, use it
	if c.URL != "" {
		return c.URL
	}

	// Build address from components
	return fmt.Sprintf("%s:%s", c.Host, c.Port)
}

// Validate checks if the Redis configuration is valid.
func (c *RedisConfig) Validate(environment string) error {
	// Either URL or components must be provided
	if c.URL == "" {
		// Validate host
		if err := validateHost(c.Host, "redis"); err != nil {
			return err
		}

		// Validate port
		if err := validatePort(c.Port, "redis"); err != nil {
			return err
		}

		// Require password in production for security
		if environment == EnvironmentProduction {
			if c.Password == "" {
				return fmt.Errorf("redis password is required in production environment")
			}
			if err := validatePasswordStrength(c.Password, "redis", environment); err != nil {
				return err
			}
			// Enforce TLS in production
			if !c.TLSEnabled {
				return fmt.Errorf("redis TLS must be enabled in production environment")
			}
		}
	} else {
		// Validate URL format
		if err := validateRedisURL(c.URL); err != nil {
			return fmt.Errorf("invalid redis URL: %w", err)
		}
	}

	// Validate pool settings
	if c.MinIdleConns > c.PoolSize {
		return fmt.Errorf("min_idle_conns (%d) cannot be greater than pool_size (%d)", c.MinIdleConns, c.PoolSize)
	}

	return nil
}

// IsConfigured returns true if Redis has all required configuration to connect.
func (c *RedisConfig) IsConfigured() bool {
	// Either URL is provided (may contain password)
	if c.URL != "" {
		return true
	}
	// Or host and port are provided (password validated separately for production)
	return c.Host != "" && c.Port != ""
}

// validateRedisURL validates Redis connection URL format
func validateRedisURL(redisURL string) error {
	parsed, err := parseAndValidateURL(redisURL, []string{"redis", "rediss"})
	if err != nil {
		return err
	}

	// Validate database number in path (optional, defaults to 0)
	if parsed.Path != "" && parsed.Path != "/" {
		dbStr := strings.TrimPrefix(parsed.Path, "/")
		if dbStr != "" {
			var dbNum int
			if _, err := fmt.Sscanf(dbStr, "%d", &dbNum); err != nil {
				return fmt.Errorf("database number must be a valid integer: %s", dbStr)
			}
			// Redis supports DB 0-15
			if dbNum < 0 || dbNum > 15 {
				return fmt.Errorf("database number must be between 0 and 15, got %d", dbNum)
			}
		}
	}

	return nil
}
