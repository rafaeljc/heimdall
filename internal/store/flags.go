// Package store provides the Data Access Layer (Repository) for the Heimdall application.
// It handles all direct interactions with the PostgreSQL database using the pgx driver.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Flag represents the database schema for a feature flag.
// It mirrors the 'flags' table structure.
type Flag struct {
	ID           int64     `db:"id"`
	Key          string    `db:"key"`
	Name         string    `db:"name"`
	Description  string    `db:"description"`
	Enabled      bool      `db:"enabled"`
	DefaultValue bool      `db:"default_value"`
	CreatedAt    time.Time `db:"created_at"`
	UpdatedAt    time.Time `db:"updated_at"`
}

// FlagRepository defines the interface for flag persistence operations.
// Using an interface allows for dependency injection and easier mocking in tests.
type FlagRepository interface {
	// CreateFlag inserts a new flag and populates the ID and timestamps in the struct.
	CreateFlag(ctx context.Context, f *Flag) error

	// Future methods (Placeholder for next iterations):
	// GetFlagByKey(ctx context.Context, key string) (*Flag, error)
	// ListFlags(ctx context.Context, limit, offset int) ([]Flag, int64, error)
}

// PostgresStore is the implementation of FlagRepository backed by PostgreSQL.
type PostgresStore struct {
	db *pgxpool.Pool
}

// NewPostgresStore creates a new repository instance with the given connection pool.
func NewPostgresStore(db *pgxpool.Pool) *PostgresStore {
	// We assume db is validated upstream (in the API constructor or main)
	return &PostgresStore{db: db}
}

// CreateFlag inserts a new flag into the database.
// It uses the RETURNING clause to get the server-generated ID and timestamps efficiently.
func (s *PostgresStore) CreateFlag(ctx context.Context, f *Flag) error {
	query := `
		INSERT INTO flags (key, name, description, enabled, default_value)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at
	`

	// Execute query and map return values directly to the struct fields
	err := s.db.QueryRow(ctx, query,
		f.Key,
		f.Name,
		f.Description,
		f.Enabled,
		f.DefaultValue,
	).Scan(&f.ID, &f.CreatedAt, &f.UpdatedAt)

	if err != nil {
		// Handle specific database errors explicitly.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			// Error Code 23505: unique_violation
			if pgErr.Code == "23505" {
				return fmt.Errorf("flag with key %q already exists", f.Key)
			}
		}
		return fmt.Errorf("failed to insert flag: %w", err)
	}

	return nil
}
