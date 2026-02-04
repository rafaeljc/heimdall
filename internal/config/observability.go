package config

import "time"

// ObservabilityConfig holds configuration for the observability server (metrics, probes).
type ObservabilityConfig struct {
	// Port defines where the observability server listens.
	Port string `envconfig:"PORT" default:"9090"`

	// Timeout is the unified safety valve for Read/Write/Idle operations.
	// Minimum is 6s because it needs to be higher than the Handler timeout (5s).
	Timeout time.Duration `envconfig:"TIMEOUT" default:"10s" validate:"min=6s"`

	// LivenessPath is the HTTP path for k8s liveness probe.
	LivenessPath string `envconfig:"LIVENESS_PATH" default:"/healthz"`

	// ReadinessPath is the HTTP path for k8s readiness probe.
	ReadinessPath string `envconfig:"READINESS_PATH" default:"/readyz"`

	// MetricsPath is the HTTP path for Prometheus scraping.
	MetricsPath string `envconfig:"METRICS_PATH" default:"/metrics"`

	// MetricsPollingInterval controls how often we poll infrastructure (Redis, DB, Runtime) for stats.
	// Defaults to 10s to be conservative on overhead.
	MetricsPollingInterval time.Duration `envconfig:"METRICS_POLLING_INTERVAL" default:"10s" validate:"min=1s"`
}

// Validate checks ObservabilityConfig fields for correctness.
func (o *ObservabilityConfig) Validate() error {
	if err := validatePort(o.Port, "observability"); err != nil {
		return err
	}
	return nil
}
