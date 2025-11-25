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

// Compile-time check to verify that PostgresStore implements FlagRepository.
// If the interface changes and the struct doesn't, the build fails here.
var _ FlagRepository = (*PostgresStore)(nil)

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

	// ListFlags retrieves a paginated list of flags and the total count of records.
	// It orders results by ID descending (deterministic).
	ListFlags(ctx context.Context, limit, offset int) ([]*Flag, int64, error)
}

// PostgresStore is the implementation of FlagRepository backed by PostgreSQL.
type PostgresStore struct {
	db *pgxpool.Pool
}

// NewPostgresStore creates a new repository instance with the given connection pool.
func NewPostgresStore(db *pgxpool.Pool) *PostgresStore {
	if db == nil {
		panic("store: database pool cannot be nil")
	}
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

// ListFlags retrieves a subset of flags based on pagination parameters.
// It executes two queries: one for the data and one for the total count.
func (s *PostgresStore) ListFlags(ctx context.Context, limit, offset int) ([]*Flag, int64, error) {
	// 1. Get Total Count (for pagination metadata)
	// We prioritize a separate count query over window functions (COUNT(*) OVER())
	// for simplicity and predictable performance in this specific use case.
	var total int64
	countQuery := `SELECT count(*) FROM flags`

	if err := s.db.QueryRow(ctx, countQuery).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count flags: %w", err)
	}

	// If there are no flags, return empty immediately to save the second query.
	if total == 0 {
		return []*Flag{}, 0, nil
	}

	// 2. Get Data
	query := `
		SELECT id, key, name, description, enabled, default_value, created_at, updated_at
		FROM flags
		ORDER BY id DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := s.db.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list flags: %w", err)
	}
	// Ensure rows are closed to prevent connection leaks in the pool.
	defer rows.Close()

	// Pre-allocate slice with a capacity of 'limit' to avoid resizing allocations.
	flags := make([]*Flag, 0, limit)

	for rows.Next() {
		var f Flag
		if err := rows.Scan(
			&f.ID,
			&f.Key,
			&f.Name,
			&f.Description,
			&f.Enabled,
			&f.DefaultValue,
			&f.CreatedAt,
			&f.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan flag row: %w", err)
		}
		flags = append(flags, &f)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("rows iteration error: %w", err)
	}

	return flags, total, nil
}
