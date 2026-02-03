// Package syncer implements the background worker that propagates data
// from the Control Plane (PostgreSQL) to the Data Plane (Redis).
package syncer

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/rafaeljc/heimdall/internal/cache"
	"github.com/rafaeljc/heimdall/internal/config"
	"github.com/rafaeljc/heimdall/internal/observability"
	"github.com/rafaeljc/heimdall/internal/ruleengine"
	"github.com/rafaeljc/heimdall/internal/store"
)

// hydrationConcurrency defines how many concurrent goroutines
// will be used during the hydration process.
// No local Config struct; use config.SyncerConfig

// Service orchestrates the synchronization process.
type Service struct {
	logger *slog.Logger
	config config.SyncerConfig
	repo   store.FlagRepository
	cache  cache.Service
	mu     sync.Mutex
}

// New creates a new Syncer service.
func New(logger *slog.Logger, cfg config.SyncerConfig, repo store.FlagRepository, cacheSvc cache.Service) *Service {
	if logger == nil {
		logger = slog.Default()
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

	// 3. Start Queue Monitor
	go s.runQueueMonitor(ctx, 0) // Default interval

	// 4. Start Consumer
	return s.runConsumer(ctx)
}

// RunQueueMonitorOnly starts only the queue monitor for testing purposes.
// This is useful for testing the queue monitoring and metrics collection
// without running the full synchronization pipeline.
// If interval is <= 0, it defaults to 5 seconds.
func (s *Service) RunQueueMonitorOnly(ctx context.Context, interval time.Duration) error {
	s.logger.Info("starting syncer service in queue monitor only mode")

	// Run only the queue monitor
	go s.runQueueMonitor(ctx, interval)

	return nil
}

// runWatchdog periodically checks if the cache is hydrated and triggers hydration if needed.
func (s *Service) runWatchdog(ctx context.Context) {
	timer := time.NewTimer(s.config.HydrationCheckInterval)
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
			timer.Reset(s.config.HydrationCheckInterval)
		}
	}
}

// runQueueMonitor monitors the Redis queue depth for backpressure.
// It is designed to be run in a separate goroutine controlled by the caller.
//
// Usage: `go memoryCache.RunMetricsCollector(ctx, 5*time.Second)`
// If interval is <= 0, it defaults to 5 seconds.
func (s *Service) runQueueMonitor(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second // Sane default
	}

	timer := time.NewTimer(interval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			count, err := s.cache.QueueLen(ctx)
			if err != nil {
				s.logger.Error("failed to get queue depth", slog.String("error", err.Error()))
			} else {
				observability.RedisQueueDepth.Set(float64(count))
			}
			timer.Reset(interval)
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

		message, err := s.cache.WaitForUpdate(ctx, s.config.PopTimeout)

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

		s.processEventWithRetry(ctx, message)
	}
}

// processEventWithRetry processes a single flag update with retry logic.
func (s *Service) processEventWithRetry(ctx context.Context, message string) {
	// Decode message to get flag key and version for processing
	flagKey, queuedVersion, enqueuedAt := cache.DecodeQueueMessage(message)

	s.logger.Info("processing update", slog.String("key", flagKey), slog.Int64("version", queuedVersion))

	var err error
	for i := 0; i <= s.config.MaxRetries; i++ {
		if err = s.syncSingleFlag(ctx, flagKey, queuedVersion); err == nil {
			s.recordJobMetrics("success", enqueuedAt)
			if ackErr := s.cache.AckUpdate(ctx, message); ackErr != nil {
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
	if dlqErr := s.cache.MoveToDLQ(ctx, message); dlqErr != nil {
		s.logger.Error("CRITICAL: failed to move to DLQ", slog.String("key", flagKey), slog.String("error", dlqErr.Error()))
		s.recordJobMetrics("failure", enqueuedAt)
	} else {
		s.recordJobMetrics("dlq", enqueuedAt)
	}
}

// recordJobMetrics handles the final observation of metrics
func (s *Service) recordJobMetrics(status string, enqueuedAt int64) {
	observability.SyncerJobsTotal.WithLabelValues(status).Inc()

	if enqueuedAt > 0 {
		// Latency = (Time inside Queue) + (Processing Time + Retries)
		latency := time.Since(time.UnixMilli(enqueuedAt)).Seconds()
		observability.SyncerJobDuration.Observe(latency)
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

	sem := make(chan struct{}, s.config.HydrationConcurrency)
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
// Only syncs if the queued version matches the database version (prevents stale writes).
// For soft-deleted flags, writes a tombstone to Redis instead of removing the key.
func (s *Service) syncSingleFlag(ctx context.Context, key string, queuedVersion int64) error {
	// Use GetFlagByKeyIncludingDeleted to distinguish soft-delete from "never existed"
	flag, err := s.repo.GetFlagByKeyIncludingDeleted(ctx, key)
	if err != nil {
		// Check if the flag truly doesn't exist (never created)
		if isNotFoundError(err) {
			s.logger.Info("flag does not exist, skipping", slog.String("key", key))
			return nil
		}
		return err
	}

	// Version matching: only sync if queued version matches current DB version
	if flag.Version != queuedVersion {
		s.logger.Info("skipping stale event",
			slog.String("key", key),
			slog.Int64("queued_version", queuedVersion),
			slog.Int64("db_version", flag.Version))
		return nil
	}

	// Handle soft-deleted flags: convert to tombstone (clear unnecessary data)
	if flag.IsDeleted {
		s.logger.Info("flag soft-deleted, writing tombstone",
			slog.String("key", key),
			slog.Int64("version", flag.Version))

		// Clear unnecessary fields for tombstone
		flag.Name = ""
		flag.Description = ""
		flag.Enabled = false
		flag.DefaultValue = false
		flag.Rules = []ruleengine.Rule{}
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
		IsDeleted:    f.IsDeleted,
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
