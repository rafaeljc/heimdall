//go:build integration

// Package store_test contains integration tests for the Data Access Layer.
// We use the '_test' suffix to enforce black-box testing, ensuring we only
// access the exported API of the store package.
package store_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/rafaeljc/heimdall/internal/ruleengine"
	"github.com/rafaeljc/heimdall/internal/store"
	"github.com/rafaeljc/heimdall/internal/testsupport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPostgresStore_Integration orchestrates the integration tests for the repository.
// It spins up a real PostgreSQL container once and runs scenarios against it.
func TestPostgresStore_Integration(t *testing.T) {
	// 1. Infrastructure Setup
	ctx := context.Background()

	// Relative path from 'internal/store' to the 'migrations' folder in root.
	// Note: In CI/CD, ensure the working directory allows this traversal.
	migrationsPath := "../../migrations"

	pgContainer, err := testsupport.StartPostgresContainer(ctx, migrationsPath)
	require.NoError(t, err, "failed to start postgres container")

	// Ensure resource cleanup even if tests fail
	defer func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}()

	// Initialize the Repository with the real pool
	repo := store.NewPostgresStore(pgContainer.DB)

	// 2. Scenarios
	// We run these sequentially as they share the same container state.

	t.Run("CreateFlag_Success_WithDefaults", func(t *testing.T) {
		// Arrange
		inputFlag := &store.Flag{
			Key:          "integration-test-flag",
			Name:         "Integration Test",
			Description:  "Created via Testcontainers",
			Enabled:      true,
			DefaultValue: true,
			// Rules is nil here to test default behavior
		}

		// Act
		err := repo.CreateFlag(ctx, inputFlag)

		// Assert 1: Smoke Check
		require.NoError(t, err)
		assert.NotZero(t, inputFlag.ID, "expected DB to assign an ID")
		assert.False(t, inputFlag.CreatedAt.IsZero(), "expected DB to assign CreatedAt")
		assert.False(t, inputFlag.UpdatedAt.IsZero(), "expected DB to assign UpdatedAt")

		// Assert 2: Milestone 2 Defaults
		assert.Equal(t, int64(1), inputFlag.Version, "new flags must start at Version 1")
		assert.NotNil(t, inputFlag.Rules, "Rules should be initialized to empty slice")
		assert.Empty(t, inputFlag.Rules, "Rules should be empty")

		// Assert 3: Deep Verification
		// We query the DB directly to prove persistence and data integrity.
		var persistedFlag store.Flag
		query := `
			SELECT key, name, description, enabled, default_value, rules, version
			FROM flags 
			WHERE id = $1
		`
		err = pgContainer.DB.QueryRow(ctx, query, inputFlag.ID).Scan(
			&persistedFlag.Key,
			&persistedFlag.Name,
			&persistedFlag.Description,
			&persistedFlag.Enabled,
			&persistedFlag.DefaultValue,
			&persistedFlag.Rules,
			&persistedFlag.Version,
		)
		require.NoError(t, err, "failed to fetch created flag from DB for verification")

		// Compare Input vs Persisted (Field by Field)
		assert.Equal(t, inputFlag.Key, persistedFlag.Key)
		assert.Equal(t, inputFlag.Name, persistedFlag.Name)
		assert.Equal(t, inputFlag.Description, persistedFlag.Description)
		assert.Equal(t, inputFlag.Enabled, persistedFlag.Enabled)
		assert.Equal(t, inputFlag.DefaultValue, persistedFlag.DefaultValue)
		assert.Equal(t, int64(1), persistedFlag.Version)
		assert.Empty(t, persistedFlag.Rules)
	})

	t.Run("CreateFlag_Success_WithRules", func(t *testing.T) {
		// Arrange: Create a flag WITH targeting rules
		rules := []ruleengine.Rule{
			{
				ID:   "rule-1",
				Type: ruleengine.RuleTypeUserIDList,
				// We don't need real JSON content for DB test, just valid structure
			},
		}

		inputFlag := &store.Flag{
			Key:   "flag-with-rules-" + fmt.Sprint(time.Now().UnixNano()),
			Name:  "Flag Rules",
			Rules: rules,
		}

		// Act
		err := repo.CreateFlag(ctx, inputFlag)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, int64(1), inputFlag.Version)

		// Fetch back to verify JSONB round-trip
		var persistedRules []ruleengine.Rule
		query := `
			SELECT rules
			FROM flags 
			WHERE id = $1
		`
		err = pgContainer.DB.QueryRow(ctx, query, inputFlag.ID).Scan(
			&persistedRules,
		)
		require.NoError(t, err)

		require.Len(t, persistedRules, 1)
		assert.Equal(t, "rule-1", persistedRules[0].ID)
		assert.Equal(t, ruleengine.RuleTypeUserIDList, persistedRules[0].Type)
	})

	t.Run("CreateFlag_DuplicateKey_ShouldFail", func(t *testing.T) {
		// Arrange
		// We create a unique key specific to this test case to ensure isolation.
		duplicateKey := "conflict-test-key"

		initialFlag := &store.Flag{
			Key:  duplicateKey,
			Name: "Original",
		}

		// Pre-condition: The flag must exist for us to test the conflict.
		err := repo.CreateFlag(ctx, initialFlag)
		require.NoError(t, err, "failed to seed initial flag for conflict test")

		// Arrange the duplicate attempt
		dupFlag := &store.Flag{
			Key:  duplicateKey, // Same key
			Name: "Duplicate",
		}

		// Act
		err = repo.CreateFlag(ctx, dupFlag)

		// Assert
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists", "expected conflict error message")
	})

	t.Run("ListFlags_Pagination", func(t *testing.T) {
		// Arrange: Seed enough data to force pagination (Create 15 items)
		// We use a unique prefix for this test to avoid collisions or confusion
		// with data from other tests.
		itemsToCreate := 15
		pageSize := 10

		for i := range itemsToCreate {
			f := &store.Flag{
				Key:  fmt.Sprintf("pagination-test-%d", i),
				Name: fmt.Sprintf("Pagination Flag %d", i),
			}
			err := repo.CreateFlag(ctx, f)
			require.NoError(t, err, "failed to seed pagination data")
		}

		// Act: Get Page 1
		// We expect the newest items (ID DESC), so our newly created flags should appear first.
		flags, total, err := repo.ListFlags(ctx, pageSize, 0)

		// Assert
		require.NoError(t, err)

		// We expect AT LEAST the 15 items we created.
		// If previous tests created items, total will be higher, which is fine.
		// But if we run this test in isolation, it must still pass (total >= 15).
		assert.GreaterOrEqual(t, total, int64(itemsToCreate), "total count should reflect seeded data")

		// Verify Page Size Limit
		assert.Len(t, flags, pageSize, "should return exactly the page size limit")

		// Verify Deterministic Ordering (ID DESC) for the WHOLE page.
		// We iterate through the slice ensuring that every item is newer (larger ID)
		// than the item that follows it.
		for i := 0; i < len(flags)-1; i++ {
			currentID := flags[i].ID
			nextID := flags[i+1].ID

			assert.True(t, currentID > nextID,
				"ordering violation at index %d: ID %d should be > %d", i, currentID, nextID)
		}
	})

	t.Run("ListAllFlags_SyncerMode", func(t *testing.T) {
		// Arrange: Seed specific data for this scenario to ensure isolation
		createdIDs := make(map[int64]struct{})

		for i := range 5 {
			f := &store.Flag{
				Key:  fmt.Sprintf("%s-%d", "syncer-test", i),
				Name: fmt.Sprintf("Syncer Test Flag %d", i),
			}
			err := repo.CreateFlag(ctx, f)
			require.NoError(t, err, "failed to seed syncer data")
			createdIDs[f.ID] = struct{}{}
		}

		// Act
		flags, err := repo.ListAllFlags(ctx)

		// Assert
		require.NoError(t, err)
		assert.NotEmpty(t, flags)

		// Validation 1: Completeness
		// We must find ALL the flags we just created in the returned list.
		// Since the DB might contain data from other tests, we search for our specific IDs.
		foundCount := 0
		for _, f := range flags {
			if _, exists := createdIDs[f.ID]; exists {
				foundCount++
			}
		}
		assert.Equal(t, len(createdIDs), foundCount, "ListAllFlags should return all persisted flags")

		// Validation 2: Deterministic Ordering (ASC)
		// Unlike pagination (DESC), the Syncer usually reads oldest-to-newest (ASC)
		// or simply needs a stable sort order. The query specifies ORDER BY id ASC.
		for i := 0; i < len(flags)-1; i++ {
			currentID := flags[i].ID
			nextID := flags[i+1].ID

			assert.True(t, currentID < nextID,
				"ordering violation at index %d: ID %d should be < %d (ASC)", i, currentID, nextID)
		}
	})
}
