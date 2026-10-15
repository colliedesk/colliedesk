-- The Free plan and its monthly hold are gone: no code reads or writes conversations.held_at any
-- more, and the image in production never had the hold code. No conversation is deleted; only the
-- column and its index go.
DROP INDEX IF EXISTS conversations_held_idx;
ALTER TABLE conversations DROP COLUMN IF EXISTS held_at;
