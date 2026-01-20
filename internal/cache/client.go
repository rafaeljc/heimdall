package cache

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/rafaeljc/heimdall/internal/config"
	"github.com/rafaeljc/heimdall/internal/logger"
)

// NewRedisClient initializes a new Redis client connection using the provided configuration.
// It handles connection pooling, TLS, and initial connectivity checks with retries.
// This function belongs to the Infrastructure Layer.
func NewRedisClient(ctx context.Context, cfg *config.RedisConfig) (*redis.Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("redis config cannot be nil")
	}

	opts := &redis.Options{
		Addr:            cfg.Address(),
		Password:        cfg.Password,
		DB:              cfg.DB,
		DialTimeout:     cfg.DialTimeout,
		ReadTimeout:     cfg.ReadTimeout,
		WriteTimeout:    cfg.WriteTimeout,
		PoolSize:        cfg.PoolSize,
		MinIdleConns:    cfg.MinIdleConns,
		PoolTimeout:     cfg.PoolTimeout,
		MaxRetries:      cfg.MaxRetries,
		MinRetryBackoff: cfg.MinRetryBackoff,
		MaxRetryBackoff: cfg.MaxRetryBackoff,
	}

	// Configure TLS if enabled
	if cfg.TLSEnabled {
		opts.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	client := redis.NewClient(opts)

	// Retry ping with exponential backoff
	maxRetries := cfg.PingMaxRetries
	backoff := cfg.PingBackoff
	timeout := backoff * ((2 << (maxRetries - 1)) - 1) // Max timeout for context

	var lastErr error
	log := logger.FromContext(ctx)

	for attempt := 1; attempt <= maxRetries; attempt++ {
		log.Info("redis ping attempt", slog.Int("attempt", attempt), slog.Int("max_retries", maxRetries))

		initCtx, cancel := context.WithTimeout(ctx, timeout)
		pingErr := client.Ping(initCtx).Err()
		cancel()

		if pingErr == nil {
			log.Info("redis ping successful", slog.Int("attempt", attempt))
			return client, nil
		}

		log.Warn("redis ping failed", slog.Int("attempt", attempt), slog.Any("error", pingErr))
		lastErr = pingErr
		if attempt < maxRetries {
			log.Info("redis waiting before next attempt", slog.Duration("backoff", backoff))
			time.Sleep(backoff)
			backoff *= 2
		}
	}

	return nil, fmt.Errorf("failed to connect to redis after %d retries: %w", maxRetries, lastErr)
}
