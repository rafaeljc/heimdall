// Package store provides the Data Access Layer (Repository) for the Heimdall application.
// It handles all direct interactions with the PostgreSQL database using the pgx driver.
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafaeljc/heimdall/internal/ruleengine"
)

// Compile-time check to verify that PostgresStore implements FlagRepository.
// If the interface changes and the struct doesn't, the build fails here.
var _ FlagRepository = (*PostgresStore)(nil)

// Flag represents the database schema for a feature flag.
// It mirrors the 'flags' table structure.
type Flag struct {
	ID           int64  `db:"id"`
	Key          string `db:"key"`
	Name         string `db:"name"`
	Description  string `db:"description"`
	Enabled      bool   `db:"enabled"`
	DefaultValue bool   `db:"default_value"`

	// Rules stores the complex targeting logic (JSONB in DB).
	// pgx handles the mapping from JSONB to this slice automatically.
	Rules []ruleengine.Rule `db:"rules"`

	// Version is used for optimistic locking to prevent lost updates
	// in the distributed system (Redis overwrites).
	Version int64 `db:"version"`

	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// UpdateFlagParams defines the parameters for partial flag updates (PATCH semantics).
// Only non-nil fields will be updated in the database.
type UpdateFlagParams struct {
	// Key identifies which flag to update (required).
	Key string

	// Version is required for optimistic locking.
	Version int64

	// Fields to update (nil = no change).
	Name         *string
	Description  *string
	Enabled      *bool
	DefaultValue *bool
	Rules        *[]ruleengine.Rule
}

// FlagRepository defines the interface for flag persistence operations.
// Using an interface allows for dependency injection and easier mocking in tests.
type FlagRepository interface {
	// CreateFlag inserts a new flag and populates the ID and timestamps in the struct.
	CreateFlag(ctx context.Context, f *Flag) error

	// ListFlags retrieves a paginated list of flags and the total count of records.
	// It orders results by ID descending (deterministic).
	ListFlags(ctx context.Context, limit, offset int) ([]*Flag, int64, error)

	// ListAllFlags retrieves all flags from the database.
	// Used by the Syncer to populate the cache.
	ListAllFlags(ctx context.Context) ([]*Flag, error)

	// GetFlagByKey retrieves a flag by its unique key.
	GetFlagByKey(ctx context.Context, key string) (*Flag, error)

	// UpdateFlag performs a partial update with optimistic locking.
	// Returns the updated flag or an error if version mismatch or flag not found.
	UpdateFlag(ctx context.Context, params *UpdateFlagParams) (*Flag, error)

	// DeleteFlag permanently removes a flag from the database by its key.
	// Returns an error if the flag doesn't exist.
	DeleteFlag(ctx context.Context, key string) error
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
	// Ensure Rules is not nil to avoid DB constraint errors if the driver behaves strictly,
	// though our migration sets DEFAULT '[]'.
	if f.Rules == nil {
		f.Rules = []ruleengine.Rule{}
	}

	// We do NOT insert 'version' explicitly, letting the DB set DEFAULT 1.
	// We scan it back via RETURNING to keep the struct in sync.
	query := `
		INSERT INTO flags (key, name, description, enabled, default_value, rules)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, version, created_at, updated_at
	`

	// Execute query and map return values directly to the struct fields
	err := s.db.QueryRow(ctx, query,
		f.Key,
		f.Name,
		f.Description,
		f.Enabled,
		f.DefaultValue,
		f.Rules, // pgx automatically marshals this to JSONB
	).Scan(&f.ID, &f.Version, &f.CreatedAt, &f.UpdatedAt)

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
		SELECT id, key, name, description, enabled, default_value, rules, version, created_at, updated_at
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
			&f.Rules,
			&f.Version,
			&f.CreatedAt,
			&f.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan flag row: %w", err)
		}

		if len(f.Rules) == 0 {
			f.Rules = []ruleengine.Rule{}
		}

		flags = append(flags, &f)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("rows iteration error: %w", err)
	}

	return flags, total, nil
}

// ListAllFlags retrieves all flags ordered by ID.
// Warning: In a massive production DB, this should be batched.
// For the V1 "Walking Skeleton", fetching all is acceptable.
func (s *PostgresStore) ListAllFlags(ctx context.Context) ([]*Flag, error) {
	query := `
		SELECT id, key, name, description, enabled, default_value, rules, version, created_at, updated_at
		FROM flags
		ORDER BY id ASC
	`

	rows, err := s.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list all flags: %w", err)
	}
	defer rows.Close()

	var flags []*Flag
	for rows.Next() {
		var f Flag
		if err := rows.Scan(
			&f.ID,
			&f.Key,
			&f.Name,
			&f.Description,
			&f.Enabled,
			&f.DefaultValue,
			&f.Rules,
			&f.Version,
			&f.CreatedAt,
			&f.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan flag: %w", err)
		}

		if len(f.Rules) == 0 {
			f.Rules = []ruleengine.Rule{}
		}

		flags = append(flags, &f)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return flags, nil
}

// GetFlagByKey retrieves a single flag by its unique key.
func (s *PostgresStore) GetFlagByKey(ctx context.Context, key string) (*Flag, error) {
	query := `
		SELECT id, key, name, description, enabled, default_value, rules, version, created_at, updated_at
		FROM flags
		WHERE key = $1
	`

	var f Flag
	err := s.db.QueryRow(ctx, query, key).Scan(
		&f.ID,
		&f.Key,
		&f.Name,
		&f.Description,
		&f.Enabled,
		&f.DefaultValue,
		&f.Rules,
		&f.Version,
		&f.CreatedAt,
		&f.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("flag not found: %w", err)
		}
		return nil, fmt.Errorf("failed to get flag: %w", err)
	}

	if len(f.Rules) == 0 {
		f.Rules = []ruleengine.Rule{}
	}

	return &f, nil
}

// UpdateFlag performs a partial update on a flag with optimistic locking.
// Only the fields provided in params (non-nil pointers) will be updated.
// The version field is used to prevent lost updates in concurrent scenarios.
func (s *PostgresStore) UpdateFlag(ctx context.Context, params *UpdateFlagParams) (*Flag, error) {
	// Build dynamic SQL query based on provided fields
	setClauses := []string{}
	args := []interface{}{}
	argCounter := 1

	if params.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argCounter))
		args = append(args, *params.Name)
		argCounter++
	}

	if params.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argCounter))
		args = append(args, *params.Description)
		argCounter++
	}

	if params.Enabled != nil {
		setClauses = append(setClauses, fmt.Sprintf("enabled = $%d", argCounter))
		args = append(args, *params.Enabled)
		argCounter++
	}

	if params.DefaultValue != nil {
		setClauses = append(setClauses, fmt.Sprintf("default_value = $%d", argCounter))
		args = append(args, *params.DefaultValue)
		argCounter++
	}

	if params.Rules != nil {
		rules := *params.Rules
		if rules == nil {
			rules = []ruleengine.Rule{}
		}
		setClauses = append(setClauses, fmt.Sprintf("rules = $%d", argCounter))
		args = append(args, rules)
		argCounter++
	}

	// Always increment version and update timestamp
	setClauses = append(setClauses, "version = version + 1")
	setClauses = append(setClauses, "updated_at = NOW()")

	// Build the final query
	query := fmt.Sprintf(`
		UPDATE flags
		SET %s
		WHERE key = $%d AND version = $%d
		RETURNING id, key, name, description, enabled, default_value, rules, version, created_at, updated_at
	`, strings.Join(setClauses, ", "), argCounter, argCounter+1)

	// Add WHERE clause parameters
	args = append(args, params.Key, params.Version)

	// Execute the update
	var f Flag
	err := s.db.QueryRow(ctx, query, args...).Scan(
		&f.ID,
		&f.Key,
		&f.Name,
		&f.Description,
		&f.Enabled,
		&f.DefaultValue,
		&f.Rules,
		&f.Version,
		&f.CreatedAt,
		&f.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("version conflict or flag not found")
		}
		return nil, fmt.Errorf("failed to update flag: %w", err)
	}

	if len(f.Rules) == 0 {
		f.Rules = []ruleengine.Rule{}
	}

	return &f, nil
}

// DeleteFlag permanently removes a flag from the database.
// It returns an error if the flag doesn't exist.
func (s *PostgresStore) DeleteFlag(ctx context.Context, key string) error {
	query := `DELETE FROM flags WHERE key = $1`

	result, err := s.db.Exec(ctx, query, key)
	if err != nil {
		return fmt.Errorf("failed to delete flag: %w", err)
	}

	// Check if any rows were affected
	if result.RowsAffected() == 0 {
		return fmt.Errorf("flag not found")
	}

	return nil
}
