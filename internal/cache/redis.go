// Package cache provides the caching layer for the Heimdall system.
// It abstracts the interaction with the Redis L2 cache, handling serialization,
// key namespacing, and connection management.
package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// KeyPrefix is the namespace used for all flag keys in Redis.
// Example: "flag:new-checkout-flow"
const KeyPrefix = "flag"

// Service defines the interface for cache operations.
// This interface allows for dependency injection and mocking in tests.
type Service interface {
	// SetFlag updates the hash fields for a specific flag.
	SetFlag(ctx context.Context, key string, fields map[string]interface{}) error

	// HealthCheck pings the redis server to ensure connectivity.
	HealthCheck(ctx context.Context) error

	// Close terminates the connection.
	Close() error
}

// RedisCache implements CacheService using the go-redis library.
type RedisCache struct {
	client *redis.Client
}

// NewRedisCache initializes a new Redis client.
func NewRedisCache(ctx context.Context, addr string) (*RedisCache, error) {
	if addr == "" {
		return nil, fmt.Errorf("redis address cannot be empty")
	}

	opts := &redis.Options{
		Addr: addr,
		// Timeouts prevent cascading failures
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		// Connection Pool settings
		PoolSize:     10,
		MinIdleConns: 2,
	}

	client := redis.NewClient(opts)

	// Fail Fast: Verify connection immediately
	initCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := client.Ping(initCtx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	return &RedisCache{client: client}, nil
}

// SetFlag stores the flag data as a Hash in Redis (HSET).
// It uses a pipeline to ensure atomicity if multiple fields are set,
// though HSET is atomic by default for multiple fields in Redis 4.0+.
func (c *RedisCache) SetFlag(ctx context.Context, key string, fields map[string]interface{}) error {
	redisKey := fmt.Sprintf("%s:%s", KeyPrefix, key)

	// Using HSet to store the map directly.
	// go-redis handles the serialization of basic types (string, bool, int).
	if err := c.client.HSet(ctx, redisKey, fields).Err(); err != nil {
		return fmt.Errorf("failed to set flag %q in cache: %w", key, err)
	}

	return nil
}

// HealthCheck verifies the connection to the Redis server.
func (c *RedisCache) HealthCheck(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// Close closes the Redis client connection.
func (c *RedisCache) Close() error {
	return c.client.Close()
}
