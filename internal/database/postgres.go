// Package database provides low-level connections and connection pooling
// for the Heimdall services. It currently supports PostgreSQL using pgx/v5.
package database

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// pool is the singleton connection pool instance.
	// It is private to ensure access is controlled via the GetPool() function.
	pool *pgxpool.Pool

	// once ensures the initialization logic runs only once, making Connect() thread-safe.
	once sync.Once
)

// Connect initializes the PostgreSQL connection pool using the provided connection string.
// It implements the Singleton pattern: subsequent calls will return the existing pool
// without re-initializing (or returning an error).
//
// It enforces strict timeouts and connection limits.
func Connect(ctx context.Context, connString string) (*pgxpool.Pool, error) {
	var err error

	once.Do(func() {
		// 1. Parse the configuration string
		config, parseErr := pgxpool.ParseConfig(connString)
		if parseErr != nil {
			err = fmt.Errorf("failed to parse database config: %w", parseErr)
			return
		}

		// 2. Configure settings (Pool Tuning)
		// MaxConns prevents the app from starving the DB (connection exhaustion).
		// MinConns keeps some connections warm to reduce latency for new requests.
		config.MaxConns = 25
		config.MinConns = 2
		config.MaxConnLifetime = 1 * time.Hour
		config.MaxConnIdleTime = 30 * time.Minute

		// 3. Create the pool with a short timeout for fail-fast behavior
		initCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		pool, parseErr = pgxpool.NewWithConfig(initCtx, config)
		if parseErr != nil {
			err = fmt.Errorf("failed to create connection pool: %w", parseErr)
			return
		}

		// 4. Verify connection (Ping) immediately
		if pingErr := pool.Ping(initCtx); pingErr != nil {
			err = fmt.Errorf("failed to ping database: %w", pingErr)
			return
		}

		log.Println("Successfully connected to PostgreSQL")
	})

	return pool, err
}

// Close terminates the database connection pool.
// This should be called during the application's Graceful Shutdown sequence.
func Close() {
	if pool != nil {
		pool.Close()
		log.Println("Closed PostgreSQL connection pool")
	}
}

// GetPool returns the initialized connection pool instance.
// Use this function to inject the database dependency into repositories or handlers.
func GetPool() *pgxpool.Pool {
	return pool
}
