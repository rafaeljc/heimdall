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
			SELECT key, name, description, enabled, default_value, rules, version, is_deleted
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
			&persistedFlag.IsDeleted,
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
		assert.False(t, persistedFlag.IsDeleted, "new flags should not be deleted")
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

	t.Run("UpdateFlag_PartialUpdate_NameOnly", func(t *testing.T) {
		// Arrange: Create a flag
		flag := &store.Flag{
			Key:          "update-name-only-" + fmt.Sprint(time.Now().UnixNano()),
			Name:         "Original Name",
			Description:  "Original Description",
			Enabled:      false,
			DefaultValue: false,
		}
		err := repo.CreateFlag(ctx, flag)
		require.NoError(t, err)
		require.Equal(t, int64(1), flag.Version)

		// Act: Update only the name
		newName := "Updated Name"
		params := &store.UpdateFlagParams{
			Key:     flag.Key,
			Version: flag.Version,
			Name:    &newName,
		}

		updated, err := repo.UpdateFlag(ctx, params)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, "Updated Name", updated.Name)
		assert.Equal(t, "Original Description", updated.Description, "description should remain unchanged")
		assert.False(t, updated.Enabled, "enabled should remain unchanged")
		assert.False(t, updated.DefaultValue, "default_value should remain unchanged")
		assert.Equal(t, int64(2), updated.Version)
	})

	t.Run("UpdateFlag_PartialUpdate_MultipleFields", func(t *testing.T) {
		// Arrange
		flag := &store.Flag{
			Key:          "update-multi-" + fmt.Sprint(time.Now().UnixNano()),
			Name:         "Original",
			Description:  "Original Desc",
			Enabled:      false,
			DefaultValue: false,
		}
		err := repo.CreateFlag(ctx, flag)
		require.NoError(t, err)

		// Act: Update name, enabled, and default_value
		newName := "New Name"
		newEnabled := true
		newDefault := true
		params := &store.UpdateFlagParams{
			Key:          flag.Key,
			Version:      flag.Version,
			Name:         &newName,
			Enabled:      &newEnabled,
			DefaultValue: &newDefault,
		}

		updated, err := repo.UpdateFlag(ctx, params)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, "New Name", updated.Name)
		assert.Equal(t, "Original Desc", updated.Description, "description should remain unchanged")
		assert.True(t, updated.Enabled)
		assert.True(t, updated.DefaultValue)
		assert.Equal(t, int64(2), updated.Version)
	})

	t.Run("UpdateFlag_AddRules", func(t *testing.T) {
		// Arrange: Create flag without rules
		flag := &store.Flag{
			Key:  "update-add-rules-" + fmt.Sprint(time.Now().UnixNano()),
			Name: "Flag Without Rules",
		}
		err := repo.CreateFlag(ctx, flag)
		require.NoError(t, err)
		assert.Empty(t, flag.Rules)

		// Act: Add rules
		newRules := []ruleengine.Rule{
			{
				ID:   "rule-1",
				Type: ruleengine.RuleTypeUserIDList,
			},
			{
				ID:   "rule-2",
				Type: ruleengine.RuleTypePercentage,
			},
		}
		params := &store.UpdateFlagParams{
			Key:     flag.Key,
			Version: flag.Version,
			Rules:   &newRules,
		}

		updated, err := repo.UpdateFlag(ctx, params)

		// Assert
		require.NoError(t, err)
		require.Len(t, updated.Rules, 2)
		assert.Equal(t, "rule-1", updated.Rules[0].ID)
		assert.Equal(t, "rule-2", updated.Rules[1].ID)
		assert.Equal(t, int64(2), updated.Version)
	})

	t.Run("UpdateFlag_ReplaceRules", func(t *testing.T) {
		// Arrange: Create flag with initial rules
		flag := &store.Flag{
			Key:  "update-replace-rules-" + fmt.Sprint(time.Now().UnixNano()),
			Name: "Flag With Rules",
			Rules: []ruleengine.Rule{
				{
					ID:   "old-rule",
					Type: ruleengine.RuleTypeUserIDList,
				},
			},
		}
		err := repo.CreateFlag(ctx, flag)
		require.NoError(t, err)

		// Act: Replace with new rules
		newRules := []ruleengine.Rule{
			{
				ID:   "new-rule",
				Type: ruleengine.RuleTypePercentage,
			},
		}
		params := &store.UpdateFlagParams{
			Key:     flag.Key,
			Version: flag.Version,
			Rules:   &newRules,
		}

		updated, err := repo.UpdateFlag(ctx, params)

		// Assert
		require.NoError(t, err)
		require.Len(t, updated.Rules, 1)
		assert.Equal(t, "new-rule", updated.Rules[0].ID)
		assert.Equal(t, ruleengine.RuleTypePercentage, updated.Rules[0].Type)
		assert.Equal(t, int64(2), updated.Version)
	})

	t.Run("UpdateFlag_ClearRules", func(t *testing.T) {
		// Arrange: Create flag with rules
		flag := &store.Flag{
			Key:  "update-clear-rules-" + fmt.Sprint(time.Now().UnixNano()),
			Name: "Flag To Clear",
			Rules: []ruleengine.Rule{
				{
					ID:   "rule-to-delete",
					Type: ruleengine.RuleTypeUserIDList,
				},
			},
		}
		err := repo.CreateFlag(ctx, flag)
		require.NoError(t, err)

		// Act: Clear rules with empty array
		emptyRules := []ruleengine.Rule{}
		params := &store.UpdateFlagParams{
			Key:     flag.Key,
			Version: flag.Version,
			Rules:   &emptyRules,
		}

		updated, err := repo.UpdateFlag(ctx, params)

		// Assert
		require.NoError(t, err)
		assert.Empty(t, updated.Rules)
		assert.Equal(t, int64(2), updated.Version)
	})

	t.Run("UpdateFlag_VersionConflict", func(t *testing.T) {
		// Arrange: Create flag
		flag := &store.Flag{
			Key:  "update-conflict-" + fmt.Sprint(time.Now().UnixNano()),
			Name: "Conflict Test",
		}
		err := repo.CreateFlag(ctx, flag)
		require.NoError(t, err)

		// Simulate concurrent update
		newName := "First Update"
		params1 := &store.UpdateFlagParams{
			Key:     flag.Key,
			Version: flag.Version,
			Name:    &newName,
		}
		_, err = repo.UpdateFlag(ctx, params1)
		require.NoError(t, err)

		// Act: Try to update with stale version
		staleName := "Stale Update"
		params2 := &store.UpdateFlagParams{
			Key:     flag.Key,
			Version: 1, // Stale version!
			Name:    &staleName,
		}

		updated, err := repo.UpdateFlag(ctx, params2)

		// Assert
		assert.Error(t, err)
		assert.Nil(t, updated)
		assert.Contains(t, err.Error(), "conflict")

		// Verify the flag wasn't updated
		fetched, err := repo.GetFlagByKey(ctx, flag.Key)
		require.NoError(t, err)
		assert.Equal(t, "First Update", fetched.Name)
		assert.Equal(t, int64(2), fetched.Version)
	})

	t.Run("UpdateFlag_NotFound", func(t *testing.T) {
		// Arrange: Non-existent flag
		newName := "Ghost Name"
		params := &store.UpdateFlagParams{
			Key:     "non-existent-" + fmt.Sprint(time.Now().UnixNano()),
			Version: 1,
			Name:    &newName,
		}

		// Act
		updated, err := repo.UpdateFlag(ctx, params)

		// Assert
		assert.Error(t, err)
		assert.Nil(t, updated)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("UpdateFlag_MultipleSequentialUpdates", func(t *testing.T) {
		// Arrange
		flag := &store.Flag{
			Key:  "update-sequential-" + fmt.Sprint(time.Now().UnixNano()),
			Name: "Version 1",
		}
		err := repo.CreateFlag(ctx, flag)
		require.NoError(t, err)

		// Act: Perform 5 sequential updates
		currentVersion := flag.Version
		for i := 2; i <= 6; i++ {
			newName := fmt.Sprintf("Version %d", i)
			params := &store.UpdateFlagParams{
				Key:     flag.Key,
				Version: currentVersion,
				Name:    &newName,
			}

			updated, err := repo.UpdateFlag(ctx, params)
			require.NoError(t, err)
			assert.Equal(t, int64(i), updated.Version)
			currentVersion = updated.Version
		}

		// Assert final state
		fetched, err := repo.GetFlagByKey(ctx, flag.Key)
		require.NoError(t, err)
		assert.Equal(t, "Version 6", fetched.Name)
		assert.Equal(t, int64(6), fetched.Version)
	})

	t.Run("UpdateFlag_BooleanFields_SetToFalse", func(t *testing.T) {
		// Arrange: Create flag with true values
		flag := &store.Flag{
			Key:          "update-bool-" + fmt.Sprint(time.Now().UnixNano()),
			Name:         "Bool Test",
			Enabled:      true,
			DefaultValue: true,
		}
		err := repo.CreateFlag(ctx, flag)
		require.NoError(t, err)

		// Act: Explicitly set to false (important to test pointer semantics)
		newEnabled := false
		newDefault := false
		params := &store.UpdateFlagParams{
			Key:          flag.Key,
			Version:      flag.Version,
			Enabled:      &newEnabled,
			DefaultValue: &newDefault,
		}

		updated, err := repo.UpdateFlag(ctx, params)

		// Assert: false values should be applied
		require.NoError(t, err)
		assert.False(t, updated.Enabled, "enabled should be explicitly set to false")
		assert.False(t, updated.DefaultValue, "default_value should be explicitly set to false")
		assert.Equal(t, int64(2), updated.Version)
	})

	t.Run("DeleteFlag_Success", func(t *testing.T) {
		// Arrange: Create a flag to delete
		flag := &store.Flag{
			Key:  "delete-test-" + fmt.Sprint(time.Now().UnixNano()),
			Name: "Flag to Delete",
		}
		err := repo.CreateFlag(ctx, flag)
		require.NoError(t, err)

		// Verify it exists
		fetched, err := repo.GetFlagByKey(ctx, flag.Key)
		require.NoError(t, err)
		assert.Equal(t, flag.Key, fetched.Key)

		// Act: Delete the flag
		deletedVersion, err := repo.DeleteFlag(ctx, flag.Key, fetched.Version)

		// Assert
		require.NoError(t, err)
		assert.Greater(t, deletedVersion, fetched.Version, "deleted version should be incremented")

		// Verify it no longer exists
		_, err = repo.GetFlagByKey(ctx, flag.Key)
		assert.Error(t, err, "flag should not exist after deletion")
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("DeleteFlag_NotFound", func(t *testing.T) {
		// Arrange: Non-existent flag key
		nonExistentKey := "non-existent-delete-" + fmt.Sprint(time.Now().UnixNano())

		// Act
		_, err := repo.DeleteFlag(ctx, nonExistentKey, 1)

		// Assert
		assert.Error(t, err, "should return error for non-existent flag")
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("DeleteFlag_WithRules", func(t *testing.T) {
		// Arrange: Create flag with rules
		flag := &store.Flag{
			Key:  "delete-with-rules-" + fmt.Sprint(time.Now().UnixNano()),
			Name: "Flag with Rules",
			Rules: []ruleengine.Rule{
				{
					ID:   "rule-1",
					Type: ruleengine.RuleTypeUserIDList,
				},
				{
					ID:   "rule-2",
					Type: ruleengine.RuleTypePercentage,
				},
			},
		}
		err := repo.CreateFlag(ctx, flag)
		require.NoError(t, err)

		// Act: Delete the flag (including its JSONB rules)
		_, err = repo.DeleteFlag(ctx, flag.Key, flag.Version)

		// Assert
		require.NoError(t, err)

		// Verify complete deletion
		_, err = repo.GetFlagByKey(ctx, flag.Key)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("DeleteFlag_DoesNotAffectOtherFlags", func(t *testing.T) {
		// Arrange: Create two flags
		flag1 := &store.Flag{
			Key:  "delete-isolation-1-" + fmt.Sprint(time.Now().UnixNano()),
			Name: "Flag 1",
		}
		flag2 := &store.Flag{
			Key:  "delete-isolation-2-" + fmt.Sprint(time.Now().UnixNano()),
			Name: "Flag 2",
		}
		err := repo.CreateFlag(ctx, flag1)
		require.NoError(t, err)
		err = repo.CreateFlag(ctx, flag2)
		require.NoError(t, err)

		// Act: Delete only flag1
		_, err = repo.DeleteFlag(ctx, flag1.Key, flag1.Version)
		require.NoError(t, err)

		// Assert: flag1 is gone, flag2 remains
		_, err = repo.GetFlagByKey(ctx, flag1.Key)
		assert.Error(t, err, "flag1 should be deleted")

		fetched, err := repo.GetFlagByKey(ctx, flag2.Key)
		require.NoError(t, err, "flag2 should still exist")
		assert.Equal(t, flag2.Key, fetched.Key)
		assert.Equal(t, flag2.Name, fetched.Name)
	})

	t.Run("DeleteFlag_IdempotencyCheck", func(t *testing.T) {
		// Arrange: Create a flag
		flag := &store.Flag{
			Key:  "delete-idempotent-" + fmt.Sprint(time.Now().UnixNano()),
			Name: "Idempotent Test",
		}
		err := repo.CreateFlag(ctx, flag)
		require.NoError(t, err)

		// Act: Delete once (should succeed)
		_, err = repo.DeleteFlag(ctx, flag.Key, flag.Version)
		require.NoError(t, err)

		// Act: Try to delete again (should fail)
		_, err = repo.DeleteFlag(ctx, flag.Key, flag.Version)

		// Assert: Second delete should fail
		assert.Error(t, err, "deleting non-existent flag should error")
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("DeleteFlag_AffectsListOperations", func(t *testing.T) {
		// Arrange: Create multiple flags
		prefix := "delete-list-test-" + fmt.Sprint(time.Now().UnixNano())
		flags := make([]*store.Flag, 3)
		for i := 0; i < 3; i++ {
			flags[i] = &store.Flag{
				Key:  fmt.Sprintf("%s-%d", prefix, i),
				Name: fmt.Sprintf("List Test Flag %d", i),
			}
			err := repo.CreateFlag(ctx, flags[i])
			require.NoError(t, err)
		}

		// Get initial count
		_, initialTotal, err := repo.ListFlags(ctx, 100, 0)
		require.NoError(t, err)
		require.GreaterOrEqual(t, initialTotal, int64(3))

		// Act: Delete one flag
		_, err = repo.DeleteFlag(ctx, flags[1].Key, flags[1].Version)
		require.NoError(t, err)

		// Assert: Total count should decrease
		afterFlags, afterTotal, err := repo.ListFlags(ctx, 100, 0)
		require.NoError(t, err)
		assert.Equal(t, initialTotal-1, afterTotal, "total count should decrease by 1")

		// Verify the deleted flag is not in the list
		for _, f := range afterFlags {
			assert.NotEqual(t, flags[1].Key, f.Key, "deleted flag should not appear in list")
		}

		// Verify other flags still exist
		foundFlag0 := false
		foundFlag2 := false
		for _, f := range afterFlags {
			if f.Key == flags[0].Key {
				foundFlag0 = true
			}
			if f.Key == flags[2].Key {
				foundFlag2 = true
			}
		}
		assert.True(t, foundFlag0, "flag 0 should still exist")
		assert.True(t, foundFlag2, "flag 2 should still exist")
	})

	t.Run("DeleteFlag_AllowsKeyReuse_AfterSoftDelete", func(t *testing.T) {
		// This test validates the partial unique index behavior from migration 003.
		// The index `idx_flags_key_active` allows the same key to exist multiple times
		// as long as only one has is_deleted = FALSE.

		// Arrange: Create a flag
		testKey := "reusable-key-" + fmt.Sprint(time.Now().UnixNano())
		originalFlag := &store.Flag{
			Key:         testKey,
			Name:        "Original Flag",
			Description: "This will be deleted",
			Enabled:     true,
		}
		err := repo.CreateFlag(ctx, originalFlag)
		require.NoError(t, err)

		// Act 1: Soft delete the flag
		deletedVersion, err := repo.DeleteFlag(ctx, testKey, originalFlag.Version)
		require.NoError(t, err)

		// Verify it's deleted (not returned by GetFlagByKey)
		_, err = repo.GetFlagByKey(ctx, testKey)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")

		// Act 2: Create a NEW flag with the SAME key
		newFlag := &store.Flag{
			Key:         testKey, // Same key as deleted flag
			Name:        "New Flag",
			Description: "This reuses the deleted key",
			Enabled:     false,
		}
		err = repo.CreateFlag(ctx, newFlag)

		// Assert: Creation should succeed due to partial unique index
		require.NoError(t, err, "should be able to reuse key after soft delete")
		assert.NotEqual(t, originalFlag.ID, newFlag.ID, "new flag should have different ID")
		assert.Greater(t, newFlag.Version, deletedVersion, "new flag version should continue incrementing from deleted flag")
		assert.False(t, newFlag.IsDeleted, "new flag should not be deleted")

		// Verify the new flag is retrievable
		fetched, err := repo.GetFlagByKey(ctx, testKey)
		require.NoError(t, err)
		assert.Equal(t, newFlag.ID, fetched.ID)
		assert.Equal(t, "New Flag", fetched.Name)
		assert.Equal(t, "This reuses the deleted key", fetched.Description)
		assert.False(t, fetched.Enabled)

		// Verify that both records exist in the database (one deleted, one active)
		var count int
		countQuery := `SELECT COUNT(*) FROM flags WHERE key = $1`
		err = pgContainer.DB.QueryRow(ctx, countQuery, testKey).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 2, count, "should have 2 records with same key (one deleted, one active)")

		// Verify the deleted flag is marked as deleted in the DB
		var deletedCount int
		deletedQuery := `SELECT COUNT(*) FROM flags WHERE key = $1 AND is_deleted = TRUE`
		err = pgContainer.DB.QueryRow(ctx, deletedQuery, testKey).Scan(&deletedCount)
		require.NoError(t, err)
		assert.Equal(t, 1, deletedCount, "should have exactly 1 deleted record")
	})
}
