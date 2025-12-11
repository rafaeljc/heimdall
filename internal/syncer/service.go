// Package syncer implements the background worker that propagates data
// from the Control Plane (PostgreSQL) to the Data Plane (Redis).
package syncer

import (
	"context"
	"log/slog"
	"time"

	"github.com/rafaeljc/heimdall/internal/cache"
	"github.com/rafaeljc/heimdall/internal/store"
)

// Config holds the configuration for the Syncer service.
type Config struct {
	// Interval is the duration between sync cycles (polling).
	Interval time.Duration
}

// Service orchestrates the synchronization process.
type Service struct {
	logger *slog.Logger
	config Config
	repo   store.FlagRepository
	cache  cache.Service
}

// New creates a new Syncer service.
func New(logger *slog.Logger, cfg Config, repo store.FlagRepository, cacheSvc cache.Service) *Service {
	if logger == nil {
		logger = slog.Default()
	}

	if repo == nil {
		panic("syncer: flag repository cannot be nil")
	}
	if cacheSvc == nil {
		panic("syncer: cache service cannot be nil")
	}

	if cfg.Interval < time.Second {
		cfg.Interval = 10 * time.Second // Safe default
	}

	return &Service{
		logger: logger,
		config: cfg,
		repo:   repo,
		cache:  cacheSvc,
	}
}

// Run starts the syncer loop. It blocks until the context is cancelled.
func (s *Service) Run(ctx context.Context) error {
	s.logger.Info("starting syncer service", slog.String("interval", s.config.Interval.String()))

	ticker := time.NewTicker(s.config.Interval)
	defer ticker.Stop()

	// Run once immediately on startup
	if err := s.sync(ctx); err != nil {
		s.logger.Error("initial sync failed", slog.String("error", err.Error()))
	}

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("syncer service stopping...")
			return nil
		case <-ticker.C:
			if err := s.sync(ctx); err != nil {
				// We log the error but don't stop the worker.
				// Retry on next tick.
				s.logger.Error("sync cycle failed", slog.String("error", err.Error()))
			}
		}
	}
}

// sync performs a single synchronization cycle.
func (s *Service) sync(ctx context.Context) error {
	start := time.Now()

	// 1. Read from Source of Truth (Postgres)
	flags, err := s.repo.ListAllFlags(ctx)
	if err != nil {
		return err
	}

	// 2. Write to Data Plane Cache (Redis)
	// In V1, we overwrite the keys.
	count := 0
	errorCount := 0

	for _, f := range flags {
		// Serialize the flag for Redis.
		// For V1, we just store the basic fields needed for evaluation.
		// In V2, this will include the compiled Rule Engine JSON.
		flagData := map[string]interface{}{
			"id":            f.ID,
			"key":           f.Key,
			"enabled":       f.Enabled,
			"default_value": f.DefaultValue,
			"updated_at":    f.UpdatedAt.Format(time.RFC3339),
		}

		if err := s.cache.SetFlag(ctx, f.Key, flagData); err != nil {
			s.logger.Warn("failed to sync flag",
				slog.String("key", f.Key),
				slog.String("error", err.Error()),
			)
			errorCount++
			continue // Try next flag, don't abort entire batch
		}
		count++
	}

	if count > 0 || errorCount > 0 {
		s.logger.Info("sync cycle completed",
			slog.Int("synced", count),
			slog.Int("errors", errorCount),
			slog.String("duration", time.Since(start).String()),
		)
	}
	return nil
}
