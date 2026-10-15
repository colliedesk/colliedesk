-- Dropping the column makes every held conversation visible again; no conversation is deleted.
DROP INDEX IF EXISTS conversations_held_idx;
ALTER TABLE conversations DROP COLUMN IF EXISTS held_at;
