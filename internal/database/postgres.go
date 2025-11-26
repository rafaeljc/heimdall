// Package database provides the PostgreSQL connection factory.
package database

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPostgresPool initializes a PostgreSQL connection pool.
// It returns the pool directly, allowing the caller to manage the lifecycle via Dependency Injection.
func NewPostgresPool(ctx context.Context, connString string) (*pgxpool.Pool, error) {
	// 1. Parse the configuration string
	config, parseErr := pgxpool.ParseConfig(connString)
	if parseErr != nil {
		return nil, fmt.Errorf("failed to parse database config: %w", parseErr)
	}

	// 2. Configure settings (Pool Tuning)
	// MaxConns prevents the app from starving the DB (connection exhaustion).
	// Minconns keeps some connections warm to reduce latency for new requests.
	config.MaxConns = 25
	config.MinConns = 2
	config.MaxConnLifetime = 1 * time.Hour
	config.MaxConnIdleTime = 30 * time.Minute

	// 3. Create the pool with a short timeout for fail-fast behavior
	initCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(initCtx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// 4. Verify connection (Ping) immediately to ensure network is healthy
	if err := pool.Ping(initCtx); err != nil {
		pool.Close() // Clean up if ping fails
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Println("Successfully connected to PostgreSQL")
	return pool, nil
}
