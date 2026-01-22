// Package cache provides the caching layer for the Heimdall system.
// It abstracts the interaction with the Redis L2 cache, handling serialization,
// key namespacing, and connection management.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/rafaeljc/heimdall/internal/ruleengine"
)

const (
	KeyPrefix           = "heimdall:flag"
	UpdateQueueKey      = "heimdall:queue:updates"
	ProcessingQueueKey  = "heimdall:queue:processing"
	DLQKey              = "heimdall:queue:dlq"
	HydrationMarkerKey  = "heimdall:sys:hydrated"
	InvalidationChannel = "heimdall:channel:invalidation"

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

		-- Case 2: Find pipe separator (only search first 20 chars since int64 max is 19 digits)
		local search_limit = math.min(20, string.len(curr_val))
		local search_str = string.sub(curr_val, 1, search_limit)
		local pipe_pos = string.find(search_str, "|", 1, true)
		
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
	SetFlagSafely(ctx context.Context, key string, value any, version int64) (SetResult, error)

	// GetFlag retrieves and deserializes a specific flag.
	// Returns nil, nil if the key does not exist.
	GetFlag(ctx context.Context, key string) (*ruleengine.FeatureFlag, error)

	// DeleteFlag removes a flag from Redis.
	DeleteFlag(ctx context.Context, key string) error

	// Queue
	PublishUpdate(ctx context.Context, flagKey string, version int64) error
	WaitForUpdate(ctx context.Context, timeout time.Duration) (string, int64, error)
	AckUpdate(ctx context.Context, message string) error
	MoveToDLQ(ctx context.Context, message string) error

	// Pub/Sub (Real-time Invalidation)
	// BroadcastUpdate notifies all Data Plane instances to invalidate their L1.
	BroadcastUpdate(ctx context.Context, flagKey string) error
	// SubscribeInvalidation returns a channel that receives keys to be invalidated.
	SubscribeInvalidation(ctx context.Context) <-chan string

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

// NewRedisCache creates a new RedisCache instance with the given Redis client.
func NewRedisCache(client *redis.Client) *RedisCache {
	return &RedisCache{
		client: client,
		script: redis.NewScript(optimisticSetScript),
	}
}

// SetFlagSafely sets the flag data in Redis using optimistic locking based on version.
func (c *RedisCache) SetFlagSafely(ctx context.Context, key string, value any, version int64) (SetResult, error) {
	redisKey := fmt.Sprintf("%s:%s", KeyPrefix, key)
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return 0, fmt.Errorf("marshal error: %w", err)
	}

	storageValue := encodeFlag(jsonBytes, version)

	res, err := c.script.Run(ctx, c.client, []string{redisKey}, storageValue, version).Int()
	if err != nil {
		return 0, fmt.Errorf("lua execution error: %w", err)
	}

	return SetResult(res), nil
}

// GetFlag retrieves and deserializes a specific flag from Redis.
func (c *RedisCache) GetFlag(ctx context.Context, key string) (*ruleengine.FeatureFlag, error) {
	redisKey := fmt.Sprintf("%s:%s", KeyPrefix, key)

	val, err := c.client.Get(ctx, redisKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil // Not found is not an error
		}
		return nil, fmt.Errorf("failed to get flag from redis: %w", err)
	}

	jsonPart := decodeFlag(val)

	var flag ruleengine.FeatureFlag
	if err := json.Unmarshal([]byte(jsonPart), &flag); err != nil {
		return nil, fmt.Errorf("failed to unmarshal flag json: %w", err)
	}

	// Compile rules: parse JSON Value into efficient CompiledValue structures
	if err := ruleengine.CompileRules(flag.Rules); err != nil {
		return nil, fmt.Errorf("failed to compile rules for flag %s: %w", key, err)
	}

	return &flag, nil
}

// DeleteFlag removes a flag from Redis.
func (c *RedisCache) DeleteFlag(ctx context.Context, key string) error {
	redisKey := fmt.Sprintf("%s:%s", KeyPrefix, key)
	return c.client.Del(ctx, redisKey).Err()
}

// PublishUpdate adds a flag update message to the queue in format "flagKey:version".
func (c *RedisCache) PublishUpdate(ctx context.Context, flagKey string, version int64) error {
	message := EncodeQueueMessage(flagKey, version)
	return c.client.LPush(ctx, UpdateQueueKey, message).Err()
}

// WaitForUpdate blocks until a flag update is available in the queue.
// Returns the flag key, version, and any error. Format: "flagKey:version".
func (c *RedisCache) WaitForUpdate(ctx context.Context, timeout time.Duration) (string, int64, error) {
	// BLMOVE atomically moves from Updates queue to Processing queue
	message, err := c.client.BLMove(ctx, UpdateQueueKey, ProcessingQueueKey, "RIGHT", "LEFT", timeout).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", 0, ErrQueueTimeout
		}
		return "", 0, err
	}

	flagKey, version := DecodeQueueMessage(message)
	return flagKey, version, nil
}

// AckUpdate removes a message from the processing queue after successful processing.
func (c *RedisCache) AckUpdate(ctx context.Context, message string) error {
	return c.client.LRem(ctx, ProcessingQueueKey, 1, message).Err()
}

// MoveToDLQ moves a message from the processing queue to the dead-letter queue.
func (c *RedisCache) MoveToDLQ(ctx context.Context, message string) error {
	// Note: LMove doesn't support removing specific values, only positional.
	// We remove from processing queue and push the message to DLQ manually.
	if err := c.client.LRem(ctx, ProcessingQueueKey, 1, message).Err(); err != nil {
		return err
	}
	return c.client.LPush(ctx, DLQKey, message).Err()
}

// BroadcastUpdate publishes an invalidation event to all Data Plane pods.
func (c *RedisCache) BroadcastUpdate(ctx context.Context, flagKey string) error {
	return c.client.Publish(ctx, InvalidationChannel, flagKey).Err()
}

// SubscribeInvalidation returns a channel that receives keys needing invalidation.
// It manages the subscription lifecycle in a background goroutine.
func (c *RedisCache) SubscribeInvalidation(ctx context.Context) <-chan string {
	ch := make(chan string)
	pubsub := c.client.Subscribe(ctx, InvalidationChannel)

	go func() {
		defer close(ch)
		defer pubsub.Close()

		// Wait for subscription to be confirmed
		if _, err := pubsub.Receive(ctx); err != nil {
			return
		}

		chMsg := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-chMsg:
				if !ok {
					return
				}
				ch <- msg.Payload
			}
		}
	}()

	return ch
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

// encodeFlag encodes a flag's JSON bytes and version into storage format: "version|json"
// Uses optimized string building to reduce allocations.
func encodeFlag(jsonBytes []byte, version int64) string {
	var buf strings.Builder
	buf.Grow(len(jsonBytes) + 20) // version (max 20 digits) + pipe + json
	buf.WriteString(strconv.FormatInt(version, 10))
	buf.WriteByte('|')
	buf.Write(jsonBytes)
	return buf.String()
}

// decodeFlag extracts the JSON part from the storage format: "version|json"
// Returns the JSON string, falling back to the raw value if no pipe separator is found.
// Optimized to search only the first 20 characters (max digits in int64).
func decodeFlag(value string) string {
	// int64 max is 19 digits, so pipe must be within first 20 chars
	searchLimit := min(len(value), 20)

	pipePos := strings.IndexByte(value[:searchLimit], '|')
	if pipePos == -1 {
		return value // Fallback for corrupted data
	}

	return value[pipePos+1:]
}

// EncodeQueueMessage encodes a flag key and version into queue message format: "flagKey:version"
func EncodeQueueMessage(flagKey string, version int64) string {
	var buf strings.Builder
	buf.Grow(len(flagKey) + 20) // flagKey + colon + version (max 20 digits)
	buf.WriteString(flagKey)
	buf.WriteByte(':')
	buf.WriteString(strconv.FormatInt(version, 10))
	return buf.String()
}

// DecodeQueueMessage extracts the flag key and version from queue message format: "flagKey:version"
// Returns the flag key and version. If parsing fails, returns the original string as key and version 0.
func DecodeQueueMessage(message string) (string, int64) {
	// Find the last colon to handle flag keys that might contain colons
	colonPos := strings.LastIndexByte(message, ':')
	if colonPos == -1 {
		return message, 0 // Fallback for legacy format (just key)
	}

	flagKey := message[:colonPos]
	versionStr := message[colonPos+1:]

	version, err := strconv.ParseInt(versionStr, 10, 64)
	if err != nil {
		return message, 0 // Fallback if version is invalid
	}

	return flagKey, version
}

// DecodeFlagForTest is a test helper that extracts the JSON portion from Redis storage.
// This is exported for use in integration tests to verify stored values.
func DecodeFlagForTest(value string) string {
	return decodeFlag(value)
}
