// Package config provides centralized configuration management for Heimdall services.
// It uses envconfig for environment variable loading and validator for validation.
package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/kelseyhightower/envconfig"
)

const (
	// EnvironmentProduction is the production environment identifier
	EnvironmentProduction = "production"
)

// Config holds the complete application configuration.
type Config struct {
	App      AppConfig      `envconfig:"APP"`
	Server   ServerConfig   `envconfig:"SERVER"`
	Database DatabaseConfig `envconfig:"DB"`
	Redis    RedisConfig    `envconfig:"REDIS"`
	Syncer   SyncerConfig   `envconfig:"SYNCER"`
	Health   HealthConfig   `envconfig:"HEALTH"`
}

// AppConfig contains core application settings.
type AppConfig struct {
	Name            string        `envconfig:"NAME" default:"heimdall"`
	Version         string        `envconfig:"VERSION" default:"dev"`
	Environment     string        `envconfig:"ENV" default:"development" validate:"oneof=development staging production"`
	LogLevel        string        `envconfig:"LOG_LEVEL" default:"info" validate:"oneof=debug info warn error"`
	LogFormat       string        `envconfig:"LOG_FORMAT" default:"text" validate:"oneof=json text"`
	ShutdownTimeout time.Duration `envconfig:"SHUTDOWN_TIMEOUT" default:"30s"`
}

// ServerConfig holds server-specific configuration.
type ServerConfig struct {
	Control ControlPlaneConfig `envconfig:"CONTROL"`
	Data    DataPlaneConfig    `envconfig:"DATA"`
}

// HealthConfig configures health check endpoints.
type HealthConfig struct {
	Enabled       bool   `envconfig:"ENABLED" default:"true"`
	Path          string `envconfig:"PATH" default:"/health"`
	LivenessPath  string `envconfig:"LIVENESS_PATH" default:"/health/live"`
	ReadinessPath string `envconfig:"READINESS_PATH" default:"/health/ready"`
}

// Load reads configuration from environment variables with the HEIMDALL prefix.
func Load() (*Config, error) {
	cfg := &Config{}

	// Load with HEIMDALL_ prefix
	if err := envconfig.Process("HEIMDALL", cfg); err != nil {
		return nil, fmt.Errorf("failed to process environment variables: %w", err)
	}

	// Validate the configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// Validate performs validation on the loaded configuration using go-playground/validator.
func (c *Config) Validate() error {
	validate := validator.New()

	if err := validate.Struct(c); err != nil {
		return fmt.Errorf("validation error: %w", err)
	}

	// Additional custom validation
	if err := c.Database.Validate(c.App.Environment); err != nil {
		return err
	}

	if err := c.Redis.Validate(c.App.Environment); err != nil {
		return err
	}

	if err := c.Server.Control.Validate(c.App.Environment); err != nil {
		return err
	}

	if err := c.Server.Data.Validate(); err != nil {
		return err
	}

	return nil
}

// LogConfig logs the current configuration (without sensitive data).
func (c *Config) LogConfig(log *slog.Logger) {
	log.Info("configuration loaded",
		slog.String("app_name", c.App.Name),
		slog.String("version", c.App.Version),
		slog.String("environment", c.App.Environment),
		slog.String("log_level", c.App.LogLevel),
		slog.String("log_format", c.App.LogFormat),
		slog.Duration("shutdown_timeout", c.App.ShutdownTimeout),
		slog.String("control_port", c.Server.Control.Port),
		slog.String("data_port", c.Server.Data.Port),
		slog.Bool("tls_enabled", c.Server.Control.TLSEnabled),
		slog.Bool("health_enabled", c.Health.Enabled),
		slog.Bool("db_configured", c.Database.IsConfigured()),
		slog.Bool("redis_configured", c.Redis.IsConfigured()),
	)
}

// Shared validation helper functions

// validatePort checks if port is valid (1-65535)
func validatePort(port, context string) error {
	if port == "" {
		return fmt.Errorf("%s port cannot be empty", context)
	}
	portNum, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("%s port must be a number: %w", context, err)
	}
	if portNum < 1 || portNum > 65535 {
		return fmt.Errorf("%s port must be between 1 and 65535, got %d", context, portNum)
	}
	return nil
}

// validateHost checks if host is not empty and contains no whitespace
func validateHost(host, context string) error {
	if host == "" {
		return fmt.Errorf("%s host cannot be empty", context)
	}
	if strings.TrimSpace(host) != host {
		return fmt.Errorf("%s host cannot contain whitespace", context)
	}
	return nil
}

// validateNoWhitespace checks if a value is not empty and contains no whitespace
func validateNoWhitespace(value, fieldName string) error {
	if value == "" {
		return fmt.Errorf("%s cannot be empty", fieldName)
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s cannot contain whitespace", fieldName)
	}
	return nil
}

// validatePasswordStrength checks password meets minimum requirements
func validatePasswordStrength(password, context, environment string) error {
	if environment == EnvironmentProduction {
		if len(password) < 12 {
			return fmt.Errorf("%s password must be at least 12 characters in production", context)
		}
	}
	return nil
}

// isSecureSSLMode checks if SSL mode is production-safe
func isSecureSSLMode(mode string) bool {
	return mode == "require" || mode == "verify-ca" || mode == "verify-full"
}

// parseAndValidateURL is a helper for parsing URLs with scheme validation
func parseAndValidateURL(rawURL string, allowedSchemes []string) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	// Validate scheme
	validScheme := slices.Contains(allowedSchemes, parsed.Scheme)
	if !validScheme {
		return nil, fmt.Errorf("invalid scheme '%s', must be one of: %v", parsed.Scheme, allowedSchemes)
	}

	// Validate host is present
	if parsed.Host == "" {
		return nil, fmt.Errorf("host is required in URL")
	}

	return parsed, nil
}
