BEGIN;

-- 1. Change 'version' column from INTEGER to BIGINT for better scalability.
-- INTEGER range: ~2.1 billion (can overflow with heavy usage)
-- BIGINT range: ~9.2 quintillion (essentially unlimited)
-- This aligns with Go's int64 type and provides future-proofing.
ALTER TABLE flags
ALTER COLUMN version TYPE BIGINT;

-- 2. Add 'is_deleted' column to implement soft delete functionality.
-- Type BOOLEAN: Simple flag to indicate deletion status.
-- Default FALSE: Existing records are not deleted.
ALTER TABLE flags
ADD COLUMN is_deleted BOOLEAN NOT NULL DEFAULT FALSE;

-- 3. Drop the existing UNIQUE constraint on 'key'.
-- The old constraint doesn't allow the same key to be used again after soft deletion.
ALTER TABLE flags 
DROP CONSTRAINT IF EXISTS flags_key_key;

-- 4. Add partial unique index on 'key' for non-deleted flags.
-- This allows the same key to be reused after soft deletion (when is_deleted = TRUE).
-- Only active flags (is_deleted = FALSE) must have unique keys.
CREATE UNIQUE INDEX IF NOT EXISTS idx_flags_key_active 
ON flags(key) 
WHERE is_deleted = FALSE;

-- 5. Add unique index on (key, version) to ensure each flag version is unique.
-- This enforces that a flag cannot have the same version number appear twice,
-- which is critical for optimistic locking integrity.
CREATE UNIQUE INDEX IF NOT EXISTS idx_flags_key_version 
ON flags(key, version);

COMMIT;
