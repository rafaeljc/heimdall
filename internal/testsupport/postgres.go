// Package testsupport provides helper functions for spinning up ephemeral
// Docker containers (PostgreSQL, Redis) for integration testing.
package testsupport

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rafaeljc/heimdall/internal/config"
	"github.com/rafaeljc/heimdall/internal/database"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// PostgresContainer holds the references to the running Docker container
// and the initialized database connection pool.
type PostgresContainer struct {
	Container        testcontainers.Container
	DB               *pgxpool.Pool
	ConnectionString string
}

// Terminate stops and removes the docker container.
func (c *PostgresContainer) Terminate(ctx context.Context) error {
	c.DB.Close()
	return c.Container.Terminate(ctx)
}

// StartPostgresContainer spins up a PostgreSQL 15-alpine container.
// It dynamically scans the migrationsDir for all .sql files and executes them
// in alphabetical order, ensuring the test DB matches the production schema.
func StartPostgresContainer(ctx context.Context, migrationsDir string) (*PostgresContainer, error) {
	// 1. Resolve absolute path
	absPath, err := filepath.Abs(migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve migrations path: %w", err)
	}

	// 2. Scan and Sort Migrations
	migrationFiles, err := getMigrationFiles(absPath)
	if err != nil {
		return nil, err
	}

	if len(migrationFiles) == 0 {
		return nil, fmt.Errorf("no migration files found in %s", absPath)
	}

	// 3. Configure Container
	dbName := "heimdall_test"
	dbUser := "testuser"
	dbPassword := "testpassword"

	pgContainer, err := postgres.Run(ctx,
		"postgres:15-alpine",
		postgres.WithDatabase(dbName),
		postgres.WithUsername(dbUser),
		postgres.WithPassword(dbPassword),
		// Automatically executes the found SQL files on startup
		postgres.WithInitScripts(migrationFiles...),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(10*time.Second),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to start postgres container: %w", err)
	}

	// 4. Get Connection String
	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, fmt.Errorf("failed to get connection string: %w", err)
	}

	// 5. Initialize Application DB Client with test config.DatabaseConfig
	testCfg := &config.DatabaseConfig{
		URL:             connStr,
		MaxConns:        5,
		MinConns:        1,
		MaxConnLifetime: 30 * time.Minute,
		MaxConnIdleTime: 5 * time.Minute,
		ConnectTimeout:  5 * time.Second,
	}
	pool, err := database.NewPostgresPool(ctx, testCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create pgx pool: %w", err)
	}

	return &PostgresContainer{
		Container:        pgContainer,
		DB:               pool,
		ConnectionString: connStr,
	}, nil
}

// getMigrationFiles reads a directory and returns a sorted list of absolute paths to .sql files.
func getMigrationFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations directory: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}

	// Sort by filename to ensure correct execution order (001, 002, etc.)
	sort.Strings(files)

	return files, nil
}
