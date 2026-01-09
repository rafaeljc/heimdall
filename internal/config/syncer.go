package config

import "time"

// SyncerConfig contains configuration for the Syncer worker service.
type SyncerConfig struct {
	Enabled                bool          `envconfig:"ENABLED" default:"true"`
	PopTimeout             time.Duration `envconfig:"POP_TIMEOUT" default:"5s" validate:"gt=0"`
	HydrationCheckInterval time.Duration `envconfig:"HYDRATION_CHECK_INTERVAL" default:"10s"`
	MaxRetries             int           `envconfig:"MAX_RETRIES" default:"3" validate:"min=0"`
	BaseRetryDelay         time.Duration `envconfig:"BASE_RETRY_DELAY" default:"1s"`
	HydrationConcurrency   int           `envconfig:"HYDRATION_CONCURRENCY" default:"10" validate:"min=1"`
}
