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

	// IsDeleted indicates whether the flag has been soft-deleted.
	// Soft deletion allows the same key to be reused after deletion.
	IsDeleted bool `db:"is_deleted"`

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

	// DeleteFlag performs a soft delete on a flag by setting is_deleted = TRUE.
	// This allows the same key to be reused in the future while preserving history.
	// Uses optimistic locking with version check to prevent concurrent deletion conflicts.
	// Increments the version and returns the new version for cache invalidation.
	// Returns an error if the flag doesn't exist, is already deleted, or version mismatch.
	DeleteFlag(ctx context.Context, key string, version int64) (int64, error)
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
// It uses a single atomic query with a CTE to calculate the next version for the key,
// ensuring version continuity when reusing keys after soft delete. The composite index
// on (key, version) makes the MAX operation efficient via index-only scan.
func (s *PostgresStore) CreateFlag(ctx context.Context, f *Flag) error {
	// Ensure Rules is not nil to avoid DB constraint errors if the driver behaves strictly,
	// though our migration sets DEFAULT '[]'.
	if f.Rules == nil {
		f.Rules = []ruleengine.Rule{}
	}

	// Single atomic query: Calculate next version and insert.
	// Uses backwards index scan (ORDER BY DESC LIMIT 1) instead of MAX for better performance.
	// The idx_flags_key_version composite index enables O(1) lookup of the highest version.
	// This is faster than MAX() because it stops at the first match without aggregation.
	query := `
		WITH next_version AS (
			SELECT COALESCE(
				(SELECT version FROM flags WHERE key = $1 ORDER BY version DESC LIMIT 1),
				0
			) + 1 AS version
		)
		INSERT INTO flags (key, name, description, enabled, default_value, rules, version, is_deleted)
		SELECT $1, $2, $3, $4, $5, $6, next_version.version, $7
		FROM next_version
		RETURNING id, version, is_deleted, created_at, updated_at
	`

	err := s.db.QueryRow(ctx, query,
		f.Key,
		f.Name,
		f.Description,
		f.Enabled,
		f.DefaultValue,
		f.Rules,
		false, // is_deleted = FALSE for new flags
	).Scan(&f.ID, &f.Version, &f.IsDeleted, &f.CreatedAt, &f.UpdatedAt)

	if err != nil {
		// Handle specific database errors explicitly.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			// Error Code 23505: unique_violation on idx_flags_key_active
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
	// Only count non-deleted flags.
	var total int64
	countQuery := `SELECT count(*) FROM flags WHERE is_deleted = FALSE`

	if err := s.db.QueryRow(ctx, countQuery).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count flags: %w", err)
	}

	// If there are no flags, return empty immediately to save the second query.
	if total == 0 {
		return []*Flag{}, 0, nil
	}

	// 2. Get Data (only non-deleted flags)
	query := `
		SELECT id, key, name, description, enabled, default_value, rules, version, is_deleted, created_at, updated_at
		FROM flags
		WHERE is_deleted = FALSE
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
			&f.IsDeleted,
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

// ListAllFlags retrieves all non-deleted flags ordered by ID.
// Warning: In a massive production DB, this should be batched.
// For the V1 "Walking Skeleton", fetching all is acceptable.
func (s *PostgresStore) ListAllFlags(ctx context.Context) ([]*Flag, error) {
	query := `
		SELECT id, key, name, description, enabled, default_value, rules, version, is_deleted, created_at, updated_at
		FROM flags
		WHERE is_deleted = FALSE
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
			&f.IsDeleted,
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

// GetFlagByKey retrieves a single non-deleted flag by its unique key.
func (s *PostgresStore) GetFlagByKey(ctx context.Context, key string) (*Flag, error) {
	query := `
		SELECT id, key, name, description, enabled, default_value, rules, version, is_deleted, created_at, updated_at
		FROM flags
		WHERE key = $1 AND is_deleted = FALSE
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
		&f.IsDeleted,
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
	args := []any{}
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

	// Single atomic query that distinguishes between not found and version conflict.
	// Uses a CTE with FULL OUTER JOIN to always return a result, even when UPDATE fails.
	// This allows us to determine the exact failure reason without a second query.
	query := fmt.Sprintf(`
		WITH flag_check AS (
			SELECT version, is_deleted
			FROM flags
			WHERE key = $%d
		),
		updated AS (
			UPDATE flags
			SET %s
			WHERE key = $%d AND version = $%d AND is_deleted = FALSE
			RETURNING id, key, name, description, enabled, default_value, rules, version, is_deleted, created_at, updated_at
		)
		SELECT 
			u.id, u.key, u.name, u.description, u.enabled, u.default_value, u.rules, u.version, u.is_deleted, u.created_at, u.updated_at,
			fc.version as existing_version,
			fc.is_deleted as existing_is_deleted
		FROM flag_check fc
		FULL OUTER JOIN updated u ON true
	`, argCounter, strings.Join(setClauses, ", "), argCounter, argCounter+1)

	// Add WHERE clause parameters
	args = append(args, params.Key, params.Version)

	// Execute the update
	// Use pointers for all update fields since they may be NULL if the UPDATE didn't match any rows
	var updateID *int64
	var updateKey *string
	var updateName *string
	var updateDescription *string
	var updateEnabled *bool
	var updateDefaultValue *bool
	var updateRules *[]ruleengine.Rule
	var updateVersion *int64
	var updateIsDeleted *bool
	var updateCreatedAt *time.Time
	var updateUpdatedAt *time.Time
	var existingVersion *int64
	var existingIsDeleted *bool

	err := s.db.QueryRow(ctx, query, args...).Scan(
		&updateID,
		&updateKey,
		&updateName,
		&updateDescription,
		&updateEnabled,
		&updateDefaultValue,
		&updateRules,
		&updateVersion,
		&updateIsDeleted,
		&updateCreatedAt,
		&updateUpdatedAt,
		&existingVersion,
		&existingIsDeleted,
	)

	if err == pgx.ErrNoRows {
		// No rows means flag doesn't exist at all
		return nil, fmt.Errorf("flag not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update flag: %w", err)
	}

	// Check if update succeeded (updateID will be non-nil if UPDATE returned a row)
	if updateID == nil {
		// Update failed - determine why based on the flag_check CTE result
		if existingVersion == nil {
			// Flag doesn't exist at all
			return nil, fmt.Errorf("flag not found")
		}
		if existingIsDeleted != nil && *existingIsDeleted {
			// Flag exists but is soft-deleted
			return nil, fmt.Errorf("flag not found")
		}
		// Flag exists and is not deleted, so must be version mismatch
		return nil, fmt.Errorf("version conflict")
	}

	// Build the result flag
	f := &Flag{
		ID:           *updateID,
		Key:          *updateKey,
		Name:         *updateName,
		Description:  *updateDescription,
		Enabled:      *updateEnabled,
		DefaultValue: *updateDefaultValue,
		Rules:        *updateRules,
		Version:      *updateVersion,
		IsDeleted:    *updateIsDeleted,
		CreatedAt:    *updateCreatedAt,
		UpdatedAt:    *updateUpdatedAt,
	}

	if len(f.Rules) == 0 {
		f.Rules = []ruleengine.Rule{}
	}

	return f, nil
}

// DeleteFlag performs a soft delete on a flag by setting is_deleted = TRUE.
// This allows the same key to be reused in the future while preserving history.
// Uses optimistic locking to prevent concurrent deletion conflicts.
// Increments the version on delete and returns the new version for cache updates.
// Returns distinct errors for not found vs version mismatch (single atomic query).
func (s *PostgresStore) DeleteFlag(ctx context.Context, key string, version int64) (int64, error) {
	// Single atomic query that distinguishes between not found and version conflict.
	// Uses a CTE with FULL OUTER JOIN to always return a result.
	query := `
		WITH flag_check AS (
			SELECT version, is_deleted
			FROM flags
			WHERE key = $1
		),
		deleted AS (
			UPDATE flags
			SET is_deleted = TRUE, version = version + 1, updated_at = NOW()
			WHERE key = $1 AND version = $2 AND is_deleted = FALSE
			RETURNING id, version
		)
		SELECT 
			d.id as deleted_id,
			d.version as new_version,
			fc.version as existing_version,
			fc.is_deleted as existing_is_deleted
		FROM flag_check fc
		FULL OUTER JOIN deleted d ON true
	`

	var deletedID *int64
	var newVersion *int64
	var existingVersion *int64
	var existingIsDeleted *bool
	err := s.db.QueryRow(ctx, query, key, version).Scan(&deletedID, &newVersion, &existingVersion, &existingIsDeleted)
	if err == pgx.ErrNoRows {
		// No rows means flag doesn't exist at all
		return 0, fmt.Errorf("flag not found")
	}
	if err != nil {
		return 0, fmt.Errorf("failed to delete flag: %w", err)
	}

	// Check if delete succeeded (deletedID will be non-nil if UPDATE returned a row)
	if deletedID == nil {
		// Delete failed - determine why based on the flag_check CTE result
		if existingVersion == nil {
			// Flag doesn't exist at all
			return 0, fmt.Errorf("flag not found")
		}
		if existingIsDeleted != nil && *existingIsDeleted {
			// Flag exists but is already soft-deleted
			return 0, fmt.Errorf("flag not found")
		}
		// Flag exists and is not deleted, so must be version mismatch
		return 0, fmt.Errorf("version conflict")
	}

	return *newVersion, nil
}
