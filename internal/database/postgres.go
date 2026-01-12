// Package database provides the PostgreSQL connection factory.
package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rafaeljc/heimdall/internal/config"
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

	// Verify connection (Ping) immediately to ensure network is healthy
	if err := pool.Ping(initCtx); err != nil {
		pool.Close() // Clean up if ping fails
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return pool, nil
}
