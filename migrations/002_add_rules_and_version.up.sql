BEGIN;

-- 1. Add 'rules' column to store complex targeting strategies.
-- Type JSONB: Better read/write performance and supports indexing.
-- Default '[]': Ensures legacy flags are treated as having "no rules" (empty list)
--               instead of NULL, preventing NullPointerExceptions in the app.
ALTER TABLE flags 
ADD COLUMN rules JSONB NOT NULL DEFAULT '[]'::jsonb;

-- 2. Add 'version' column for Optimistic Locking (Header-Based Strategy).
-- Default 1: Existing flags start at version 1.
-- The increment is managed by the application layer (API) to ensure consistency.
ALTER TABLE flags 
ADD COLUMN version INTEGER NOT NULL DEFAULT 1;

COMMIT;
