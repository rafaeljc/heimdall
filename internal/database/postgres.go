// Package database provides the PostgreSQL connection factory.
package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rafaeljc/heimdall/internal/config"
	"github.com/rafaeljc/heimdall/internal/logger"
	"github.com/rafaeljc/heimdall/internal/observability"
)

// NewPostgresPool initializes a PostgreSQL connection pool using DatabaseConfig.
// It returns the pool directly, allowing the caller to manage the lifecycle via Dependency Injection.
// All pool and connection settings are sourced from the config package.
func NewPostgresPool(ctx context.Context, dbCfg *config.DatabaseConfig) (*pgxpool.Pool, error) {
	if dbCfg == nil {
		return nil, fmt.Errorf("database config cannot be nil")
	}

	// Parse the configuration string from config package
	pgxCfg, parseErr := pgxpool.ParseConfig(dbCfg.ConnectionString())
	if parseErr != nil {
		return nil, fmt.Errorf("failed to parse database config: %w", parseErr)
	}

	// Use pool settings from config package
	pgxCfg.MaxConns = dbCfg.MaxConns
	pgxCfg.MinConns = dbCfg.MinConns
	pgxCfg.MaxConnLifetime = dbCfg.MaxConnLifetime
	pgxCfg.MaxConnIdleTime = dbCfg.MaxConnIdleTime

	// Use connect timeout from config package
	initCtx, cancel := context.WithTimeout(ctx, dbCfg.ConnectTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(initCtx, pgxCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Retry ping with exponential backoff
	maxRetries := dbCfg.PingMaxRetries
	backoff := dbCfg.PingBackoff
	var lastErr error
	log := logger.FromContext(ctx)
	for attempt := 1; attempt <= maxRetries; attempt++ {
		log.Info("database ping attempt", slog.Int("attempt", attempt), slog.Int("max_retries", maxRetries))
		if err := pool.Ping(initCtx); err == nil {
			log.Info("database ping successful", slog.Int("attempt", attempt))
			return pool, nil
		} else {
			log.Warn("database ping failed", slog.Int("attempt", attempt), slog.Any("error", err))
			lastErr = err
			if attempt < maxRetries {
				log.Info("database waiting before next attempt", slog.Duration("backoff", backoff))
				time.Sleep(backoff)
				backoff *= 2
			}
		}
	}
	pool.Close()
	return nil, fmt.Errorf("failed to ping database after %d retries: %w", maxRetries, lastErr)
}

// RunPoolMonitor periodically records connection pool statistics to Prometheus.
// It tracks pool size, usage, and acquisition latency/wait times.
// This function blocks until ctx is canceled, so it should be run in a goroutine.
//
// Usage: go database.RunPoolMonitor(ctx, pool, 10*time.Second)
func RunPoolMonitor(ctx context.Context, pool *pgxpool.Pool, interval time.Duration) {
	log := logger.FromContext(ctx).With(slog.String("component", "postgres_monitor"))
	log.Info("starting postgres pool monitor", slog.Duration("interval", interval))

	timer := time.NewTimer(interval)
	defer timer.Stop()

	// Initialize previous state to calculate deltas correctly.
	// We capture the initial state so we don't report the entire history as a sudden spike.
	initialStats := pool.Stat()
	prevAcquireCount := initialStats.AcquireCount()
	prevAcquireDuration := initialStats.AcquireDuration()
	prevWaitCount := initialStats.EmptyAcquireCount()

	for {
		select {
		case <-ctx.Done():
			log.Info("stopping postgres pool monitor")
			return
		case <-timer.C:
			stats := pool.Stat()

			// 1. Update Gauges (Absolute State)
			// Labels: state (idle, in_use, total, max)
			observability.DBPoolConnections.WithLabelValues("total").Set(float64(stats.TotalConns()))
			observability.DBPoolConnections.WithLabelValues("idle").Set(float64(stats.IdleConns()))
			observability.DBPoolConnections.WithLabelValues("in_use").Set(float64(stats.AcquiredConns()))
			observability.DBPoolConnections.WithLabelValues("max").Set(float64(stats.MaxConns()))

			// 2. Update Counters (Cumulative Deltas)
			// Since pgxpool returns total cumulative counts, and Prometheus Counters expect increments,
			// we must calculate the delta since the last tick.

			// Acquire Count
			if current := stats.AcquireCount(); current > prevAcquireCount {
				delta := float64(current - prevAcquireCount)
				observability.DBPoolAcquireCount.Add(delta)
				prevAcquireCount = current
			}

			// Acquire Duration
			if current := stats.AcquireDuration(); current > prevAcquireDuration {
				delta := (current - prevAcquireDuration).Seconds()
				observability.DBPoolAcquireDuration.Add(delta)
				prevAcquireDuration = current
			}

			// Wait Count (EmptyAcquireCount: times we had to wait for a connection)
			if current := stats.EmptyAcquireCount(); current > prevWaitCount {
				delta := float64(current - prevWaitCount)
				observability.DBPoolWaitCount.Add(delta)
				prevWaitCount = current
			}

			// Reset timer for next interval
			timer.Reset(interval)
		}
	}
}
