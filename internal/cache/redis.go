// Package cache provides the caching layer for the Heimdall system.
// It abstracts the interaction with the Redis L2 cache, handling serialization,
// key namespacing, and connection management.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	KeyPrefix          = "heimdall:flag"
	UpdateQueueKey     = "heimdall:queue:updates"
	ProcessingQueueKey = "heimdall:queue:processing"
	DLQKey             = "heimdall:queue:dlq"
	HydrationMarkerKey = "heimdall:sys:hydrated"

	// Lua script for Optimistic Locking.
	// Returns:
	// 0: Skipped (stale version)
	// 1: Updated (success)
	// 2: Repaired (corrupted data fixed)
	optimisticSetScript = `
		local key = KEYS[1]
		local new_val = ARGV[1]
		local new_ver = tonumber(ARGV[2])

		local curr_val = redis.call("GET", key)
		
		-- Case 1: New Key
		if not curr_val then
			redis.call("SET", key, new_val)
			return 1
		end

		-- Case 2: Corrupted/Legacy Data Check
		local pipe_pos = string.find(curr_val, "|")
		if not pipe_pos then
			redis.call("SET", key, new_val)
			return 2 
		end

		local curr_ver = tonumber(string.sub(curr_val, 1, pipe_pos - 1))

		-- Case 3: Version Check
		if new_ver > curr_ver then
			redis.call("SET", key, new_val)
			return 1
		end

		return 0
	`
)

type SetResult int

const (
	SetResultSkipped  SetResult = 0
	SetResultUpdated  SetResult = 1
	SetResultRepaired SetResult = 2
)

var ErrQueueTimeout = errors.New("queue timeout")

// Service defines the interface for cache operations.
// This interface allows for dependency injection and mocking in tests.
type Service interface {
	// SetFlagSafely sets the flag data with optimistic locking based on version.
	SetFlagSafely(ctx context.Context, key string, value interface{}, version int64) (SetResult, error)

	// GetFlag retrieves all fields for a specific flag hash.
	GetFlag(ctx context.Context, key string) (map[string]string, error)

	// Queue
	PublishUpdate(ctx context.Context, flagKey string) error
	WaitForUpdate(ctx context.Context, timeout time.Duration) (string, error)
	AckUpdate(ctx context.Context, flagKey string) error
	MoveToDLQ(ctx context.Context, flagKey string) error

	// Resilience
	MarkAsHydrated(ctx context.Context) error
	IsHydrated(ctx context.Context) (bool, error)

	// HealthCheck pings the redis server to ensure connectivity.
	HealthCheck(ctx context.Context) error

	// Close terminates the connection.
	Close() error
}

// RedisCache implements CacheService using the go-redis library.
type RedisCache struct {
	client *redis.Client
	script *redis.Script
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
		ReadTimeout:  30 * time.Second, // Should be longer than BLMOVE timeout
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

	return &RedisCache{
		client: client,
		script: redis.NewScript(optimisticSetScript),
	}, nil
}

// SetFlagSafely sets the flag data in Redis using optimistic locking based on version.
func (c *RedisCache) SetFlagSafely(ctx context.Context, key string, value interface{}, version int64) (SetResult, error) {
	redisKey := fmt.Sprintf("%s:%s", KeyPrefix, key)
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return 0, fmt.Errorf("marshal error: %w", err)
	}

	storageValue := fmt.Sprintf("%d|%s", version, string(jsonBytes))

	res, err := c.script.Run(ctx, c.client, []string{redisKey}, storageValue, version).Int()
	if err != nil {
		return 0, fmt.Errorf("lua execution error: %w", err)
	}

	return SetResult(res), nil
}

// GetFlag retrieves all fields from the Redis hash.
// It returns map[string]string because Redis hashes are fundamentally strings.
func (c *RedisCache) GetFlag(ctx context.Context, key string) (map[string]string, error) {
	redisKey := fmt.Sprintf("%s:%s", KeyPrefix, key)

	// HGetAll returns all fields and values of the hash.
	// If the key does not exist, it returns an empty map and no error.
	result, err := c.client.HGetAll(ctx, redisKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get flag %q from cache: %w", key, err)
	}

	return result, nil
}

// PublishUpdate adds a flag key to the update queue.
func (c *RedisCache) PublishUpdate(ctx context.Context, flagKey string) error {
	return c.client.LPush(ctx, UpdateQueueKey, flagKey).Err()
}

// WaitForUpdate blocks until a flag key is available in the update queue or timeout occurs.
func (c *RedisCache) WaitForUpdate(ctx context.Context, timeout time.Duration) (string, error) {
	// BLMOVE atomicamente move da fila de Updates para Processing
	val, err := c.client.BLMove(ctx, UpdateQueueKey, ProcessingQueueKey, "RIGHT", "LEFT", timeout).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", ErrQueueTimeout
		}
		return "", err
	}
	return val, nil
}

// AckUpdate removes a flag key from the processing queue after successful processing.
func (c *RedisCache) AckUpdate(ctx context.Context, flagKey string) error {
	return c.client.LRem(ctx, ProcessingQueueKey, 1, flagKey).Err()
}

// MoveToDLQ moves a flag key from the processing queue to the dead-letter queue.
func (c *RedisCache) MoveToDLQ(ctx context.Context, flagKey string) error {
	_, err := c.client.LMove(ctx, ProcessingQueueKey, DLQKey, "LEFT", "LEFT").Result()
	return err
}

// MarkAsHydrated sets a marker in Redis indicating that the cache has been fully hydrated.
func (c *RedisCache) MarkAsHydrated(ctx context.Context) error {
	return c.client.Set(ctx, HydrationMarkerKey, "1", 0).Err()
}

// IsHydrated checks if the hydration marker exists in Redis.
func (c *RedisCache) IsHydrated(ctx context.Context) (bool, error) {
	count, err := c.client.Exists(ctx, HydrationMarkerKey).Result()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// HealthCheck verifies the connection to the Redis server.
func (c *RedisCache) HealthCheck(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// Close closes the Redis client connection.
func (c *RedisCache) Close() error {
	return c.client.Close()
}
