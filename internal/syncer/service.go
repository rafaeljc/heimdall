// Package syncer implements the background worker that propagates data
// from the Control Plane (PostgreSQL) to the Data Plane (Redis).
package syncer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/rafaeljc/heimdall/internal/cache"
	"github.com/rafaeljc/heimdall/internal/ruleengine"
	"github.com/rafaeljc/heimdall/internal/store"
)

// hydrationConcurrency defines how many concurrent goroutines
// will be used during the hydration process.
const hydrationConcurrency = 10

// Config holds the configuration for the Syncer service.
type Config struct {
	PopTimeout     time.Duration
	HydrationCheck time.Duration
	MaxRetries     int
	BaseRetryDelay time.Duration
}

// Service orchestrates the synchronization process.
type Service struct {
	logger *slog.Logger
	config Config
	repo   store.FlagRepository
	cache  cache.Service
	mu     sync.Mutex
}

// New creates a new Syncer service.
func New(logger *slog.Logger, cfg Config, repo store.FlagRepository, cacheSvc cache.Service) *Service {
	if logger == nil {
		logger = slog.Default()
	}

	// Validate config
	if cfg.PopTimeout <= 0 {
		cfg.PopTimeout = 5 * time.Second
	}
	if cfg.HydrationCheck <= 0 {
		cfg.HydrationCheck = 10 * time.Second
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.BaseRetryDelay <= 0 {
		cfg.BaseRetryDelay = 500 * time.Millisecond
	}

	return &Service{
		logger: logger,
		config: cfg,
		repo:   repo,
		cache:  cacheSvc,
	}
}

// Run starts the Syncer service.
func (s *Service) Run(ctx context.Context) error {
	s.logger.Info("starting syncer service")

	// 1. Initial Force Hydration
	if err := s.performHydration(ctx); err != nil {
		s.logger.Error("initial hydration failed", slog.String("error", err.Error()))
	}

	// 2. Start Watchdog
	go s.runWatchdog(ctx)

	// 3. Start Consumer
	return s.runConsumer(ctx)
}

// runWatchdog periodically checks if the cache is hydrated and triggers hydration if needed.
func (s *Service) runWatchdog(ctx context.Context) {
	timer := time.NewTimer(s.config.HydrationCheck)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			hydrated, err := s.cache.IsHydrated(ctx)
			if err != nil {
				s.logger.Error("watchdog: failed to check status", slog.String("error", err.Error()))
			} else if !hydrated {
				s.logger.Warn("watchdog: redis lost memory, triggering hydration")
				if err := s.performHydration(ctx); err != nil {
					s.logger.Error("watchdog: hydration failed", slog.String("error", err.Error()))
				}
			}
			timer.Reset(s.config.HydrationCheck)
		}
	}
}

// runConsumer processes update events from the cache queue.
func (s *Service) runConsumer(ctx context.Context) error {
	s.logger.Info("entering event loop", slog.String("queue", cache.UpdateQueueKey))

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		flagKey, err := s.cache.WaitForUpdate(ctx, s.config.PopTimeout)

		// Handle context cancellation
		if ctx.Err() != nil {
			return nil
		}

		if err != nil {
			if errors.Is(err, cache.ErrQueueTimeout) {
				continue
			}
			s.logger.Error("queue wait error", slog.String("error", err.Error()))
			time.Sleep(1 * time.Second)
			continue
		}

		s.processEventWithRetry(ctx, flagKey)
	}
}

// processEventWithRetry processes a single flag update with retry logic.
func (s *Service) processEventWithRetry(ctx context.Context, flagKey string) {
	s.logger.Info("processing update", slog.String("key", flagKey))

	var err error
	for i := 0; i <= s.config.MaxRetries; i++ {
		if err = s.syncSingleFlag(ctx, flagKey); err == nil {
			if ackErr := s.cache.AckUpdate(ctx, flagKey); ackErr != nil {
				s.logger.Error("failed to ack update", slog.String("key", flagKey), slog.String("error", ackErr.Error()))
			}
			return
		}

		s.logger.Warn("sync attempt failed",
			slog.String("key", flagKey),
			slog.Int("attempt", i+1),
			slog.String("error", err.Error()))

		if i < s.config.MaxRetries {
			time.Sleep(s.config.BaseRetryDelay * time.Duration(1<<i))
		}
	}

	s.logger.Error("max retries reached, moving to DLQ", slog.String("key", flagKey))
	if dlqErr := s.cache.MoveToDLQ(ctx, flagKey); dlqErr != nil {
		s.logger.Error("CRITICAL: failed to move to DLQ", slog.String("key", flagKey), slog.String("error", dlqErr.Error()))
	}
}

// performHydration loads all flags from the repository into the cache.
func (s *Service) performHydration(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Double check to avoid redundant hydration
	if hydrated, _ := s.cache.IsHydrated(ctx); hydrated {
		s.logger.Debug("skipping hydration: redis already hydrated")
		return nil
	}

	s.logger.Info("starting full hydration")
	start := time.Now()

	flags, err := s.repo.ListAllFlags(ctx)
	if err != nil {
		return err
	}

	sem := make(chan struct{}, hydrationConcurrency)
	var wg sync.WaitGroup

	for _, f := range flags {
		wg.Add(1)
		// Acquire token (blocks if there no tokens available)
		sem <- struct{}{}

		go func(flag *store.Flag) {
			defer wg.Done()
			defer func() { <-sem }() // Release token

			if err := s.updateCacheWithRetry(ctx, flag); err != nil {
				s.logger.Warn("hydration set failed after retries",
					slog.String("key", flag.Key),
					slog.String("error", err.Error()))
			}
		}(f)
	}

	// Wait for all goroutines to finish
	wg.Wait()

	if err := s.cache.MarkAsHydrated(ctx); err != nil {
		return err
	}

	s.logger.Info("hydration completed", slog.Int("count", len(flags)), slog.String("duration", time.Since(start).String()))
	return nil
}

// updateCacheWithRetry tries to update the cache with retries.
func (s *Service) updateCacheWithRetry(ctx context.Context, f *store.Flag) error {
	var err error
	// Try up to 3 times
	for range 3 {
		err = s.updateCache(ctx, f)
		if err == nil {
			return nil
		}
		// Short and fixed backoff to avoid blocking massive hydration
		time.Sleep(50 * time.Millisecond)
	}
	return err
}

// syncSingleFlag fetches a flag from the repository and updates the cache.
// If the flag no longer exists in the database, it removes it from Redis
// and broadcasts an invalidation event to data-plane instances.
func (s *Service) syncSingleFlag(ctx context.Context, key string) error {
	flag, err := s.repo.GetFlagByKey(ctx, key)
	if err != nil {
		// Check if the flag was deleted (not found in database)
		if isNotFoundError(err) {
			s.logger.Info("flag deleted, removing from cache", slog.String("key", key))

			// Remove from Redis L2 cache
			if delErr := s.cache.DeleteFlag(ctx, key); delErr != nil {
				return fmt.Errorf("failed to delete flag from cache: %w", delErr)
			}

			// Broadcast invalidation so data-plane can clear L1 cache
			if broadcastErr := s.cache.BroadcastUpdate(ctx, key); broadcastErr != nil {
				s.logger.Error("failed to broadcast deletion",
					slog.String("key", key),
					slog.String("error", broadcastErr.Error()))
				// Don't fail the sync - cache removal succeeded
			}

			return nil
		}
		return err
	}
	return s.updateCache(ctx, flag)
}

// updateCache updates the cache with the given flag data.
func (s *Service) updateCache(ctx context.Context, f *store.Flag) error {
	domainFlag := ruleengine.FeatureFlag{
		Key:          f.Key,
		Enabled:      f.Enabled,
		DefaultValue: f.DefaultValue,
		Rules:        f.Rules,
		Version:      f.Version,
	}

	result, err := s.cache.SetFlagSafely(ctx, f.Key, domainFlag, f.Version)
	if err != nil {
		return err
	}

	if result == cache.SetResultRepaired {
		s.logger.Warn("cache data was corrupted and repaired", slog.String("key", f.Key))
	}

	return nil
}

// isNotFoundError checks if the error indicates a flag was not found in the database.
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	// Check for "flag not found" message from GetFlagByKey
	return strings.Contains(err.Error(), "flag not found")
}
