package cache

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// HealthChecker implements the observability.Checker interface for Redis.
type HealthChecker struct {
	client *redis.Client
}

// NewHealthChecker creates a new health checker for the given Redis client.
func NewHealthChecker(client *redis.Client) *HealthChecker {
	return &HealthChecker{client: client}
}

// Name returns the component name.
func (h *HealthChecker) Name() string {
	return "redis"
}

// Check verifies the Redis connection using Ping.
func (h *HealthChecker) Check(ctx context.Context) error {
	if h.client == nil {
		return fmt.Errorf("redis client is nil")
	}
	// Ping returns a PONG if healthy, or an error if connection is broken.
	return h.client.Ping(ctx).Err()
}
