package testsupport

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rafaeljc/heimdall/internal/cache"
	"github.com/rafaeljc/heimdall/internal/config"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/redis"
)

// RedisContainer holds references to the ephemeral Redis instance.
type RedisContainer struct {
	Container testcontainers.Container
	// Cache is the wrapped Heimdall cache service.
	Cache cache.Service
}

// Terminate cleans up the container and closes the client.
func (c *RedisContainer) Terminate(ctx context.Context) error {
	c.Cache.Close()
	return c.Container.Terminate(ctx)
}

// StartRedisContainer spins up a Redis 7-alpine container.
func StartRedisContainer(ctx context.Context) (*RedisContainer, error) {
	// 1. Start Container
	redisContainer, err := redis.Run(ctx,
		"redis:7-alpine",
		// Wait until "Ready to accept connections" appears in logs
		// or check port availability (redis module default).
	)
	if err != nil {
		return nil, fmt.Errorf("failed to start redis container: %w", err)
	}

	// 2. Get Connection String
	endpoint, err := redisContainer.PortEndpoint(ctx, "6379/tcp", "")
	if err != nil {
		return nil, fmt.Errorf("failed to get redis endpoint: %w", err)
	}

	// 3. Initialize Application Cache Client with test config
	// Parse host:port from endpoint (e.g., "localhost:54321")
	host, port, _ := strings.Cut(endpoint, ":")

	testCfg := &config.RedisConfig{
		Host:           host,
		Port:           port,
		Password:       "",
		DB:             0,
		PingMaxRetries: 5,
		PingBackoff:    2 * time.Second,
	}
	redisClient, err := cache.NewRedisClient(ctx, testCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create redis client: %w", err)
	}
	redisCache := cache.NewRedisCache(redisClient)

	return &RedisContainer{
		Container: redisContainer,
		Cache:     redisCache,
	}, nil
}
