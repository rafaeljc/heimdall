// Package syncer implements the background worker that propagates data
// from the Control Plane (PostgreSQL) to the Data Plane (Redis).
package syncer

import (
	"context"
	"log"
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
	config Config
	repo   store.FlagRepository
	cache  cache.Service
}

// New creates a new Syncer service.
func New(cfg Config, repo store.FlagRepository, cacheSvc cache.Service) *Service {
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
		config: cfg,
		repo:   repo,
		cache:  cacheSvc,
	}
}

// Run starts the syncer loop. It blocks until the context is cancelled.
func (s *Service) Run(ctx context.Context) error {
	log.Printf("Starting Syncer (Polling Interval: %s)", s.config.Interval)

	ticker := time.NewTicker(s.config.Interval)
	defer ticker.Stop()

	// Run once immediately on startup
	if err := s.sync(ctx); err != nil {
		log.Printf("Error in initial sync: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("Syncer stopping...")
			return nil
		case <-ticker.C:
			if err := s.sync(ctx); err != nil {
				// We log the error but don't stop the worker.
				// Retry on next tick.
				log.Printf("Error syncing flags: %v", err)
			}
		}
	}
}

// sync performs a single synchronization cycle.
func (s *Service) sync(ctx context.Context) error {
	// 1. Read from Source of Truth (Postgres)
	flags, err := s.repo.ListAllFlags(ctx)
	if err != nil {
		return err
	}

	// 2. Write to Data Plane Cache (Redis)
	// In V1, we overwrite the keys.
	count := 0
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
			log.Printf("Failed to sync flag %s: %v", f.Key, err)
			continue // Try next flag, don't abort entire batch
		}
		count++
	}

	if count > 0 {
		log.Printf("Synced %d flags to Redis", count)
	}
	return nil
}
