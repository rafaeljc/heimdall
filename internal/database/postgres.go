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
