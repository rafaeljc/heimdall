package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// DatabaseConfig contains PostgreSQL connection settings.
type DatabaseConfig struct {
	// Connection can be specified as a URL or individual components
	URL      string `envconfig:"URL"` // Full connection URL
	Host     string `envconfig:"HOST"`
	Port     string `envconfig:"PORT"`
	Name     string `envconfig:"NAME"`
	User     string `envconfig:"USER"`
	Password string `envconfig:"PASSWORD"`

	// TLS
	SSLMode string `envconfig:"SSL_MODE" default:"prefer" validate:"oneof=disable allow prefer require verify-ca verify-full"`

	// Connection Pool
	MaxConns        int           `envconfig:"MAX_CONNS" default:"25" validate:"min=1"`
	MinConns        int           `envconfig:"MIN_CONNS" default:"2" validate:"min=0"`
	MaxConnLifetime time.Duration `envconfig:"MAX_CONN_LIFETIME" default:"1h"`
	MaxConnIdleTime time.Duration `envconfig:"MAX_CONN_IDLE_TIME" default:"30m"`
	ConnectTimeout  time.Duration `envconfig:"CONNECT_TIMEOUT" default:"5s"`
}

// ConnectionString builds a PostgreSQL connection string.
// If URL is provided, it returns that. Otherwise, it constructs from components.
func (c *DatabaseConfig) ConnectionString() string {
	// If a full URL is provided, use it
	if c.URL != "" {
		return c.URL
	}

	// Build from components
	params := url.Values{}
	params.Add("sslmode", c.SSLMode)

	connStr := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?%s",
		c.User,
		c.Password,
		c.Host,
		c.Port,
		c.Name,
		params.Encode(),
	)

	return connStr
}

// Validate checks if the database configuration is valid.
func (c *DatabaseConfig) Validate(environment string) error {
	// Either URL or components must be provided for services that need DB
	if c.URL == "" {
		// Validate host
		if err := validateHost(c.Host, "database"); err != nil {
			return err
		}

		// Validate port
		if err := validatePort(c.Port, "database"); err != nil {
			return err
		}

		// Validate database name
		if err := validateDatabaseName(c.Name); err != nil {
			return err
		}

		// Validate user
		if err := validateDatabaseUser(c.User); err != nil {
			return err
		}

		// Require password in production for security
		if environment == EnvironmentProduction {
			if c.Password == "" {
				return fmt.Errorf("database password is required in production environment")
			}
			if err := validatePasswordStrength(c.Password, "database", environment); err != nil {
				return err
			}
			// Enforce secure SSL mode in production
			if !isSecureSSLMode(c.SSLMode) {
				return fmt.Errorf("database SSL mode must be 'require', 'verify-ca', or 'verify-full' in production environment")
			}
		}
	} else {
		// Validate URL format
		if err := validatePostgresURL(c.URL); err != nil {
			return fmt.Errorf("invalid database URL: %w", err)
		}
	}

	// Validate pool settings
	if c.MinConns > c.MaxConns {
		return fmt.Errorf("min_conns (%d) cannot be greater than max_conns (%d)", c.MinConns, c.MaxConns)
	}

	return nil
}

// IsConfigured returns true if database has all required configuration to connect.
func (c *DatabaseConfig) IsConfigured() bool {
	// Either URL is provided (may contain password)
	if c.URL != "" {
		return true
	}
	// Or host, port, name, and user are provided (password validated separately for production)
	return c.Host != "" && c.Port != "" && c.Name != "" && c.User != ""
}

// validatePostgresURL validates PostgreSQL connection URL format
func validatePostgresURL(dbURL string) error {
	parsed, err := parseAndValidateURL(dbURL, []string{"postgres", "postgresql"})
	if err != nil {
		return err
	}

	// Validate user is present
	if parsed.User == nil || parsed.User.Username() == "" {
		return fmt.Errorf("user is required in URL")
	}

	// Validate database name in path
	dbName := strings.TrimPrefix(parsed.Path, "/")
	if dbName == "" {
		return fmt.Errorf("database name is required in URL path")
	}

	return nil
}

// validateDatabaseName validates database name
func validateDatabaseName(name string) error {
	if err := validateNoWhitespace(name, "database name"); err != nil {
		return err
	}
	// PostgreSQL naming rules: max 63 chars
	if len(name) > 63 {
		return fmt.Errorf("database name cannot exceed 63 characters")
	}
	return nil
}

// validateDatabaseUser validates database user
func validateDatabaseUser(user string) error {
	return validateNoWhitespace(user, "database user")
}
