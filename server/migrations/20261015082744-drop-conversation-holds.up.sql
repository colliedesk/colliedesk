-- The Free plan and its monthly hold are gone: current code never reads or writes
-- conversations.held_at. Images older than the trial code (for example the rollback target
-- f2d22dd, digest 47edf4e7) still read it in the inbox list and write it for inbound mail, so this
-- must run only after no such image can be rolled back to. No conversation is deleted; only the
-- column and its index go.
DROP INDEX IF EXISTS conversations_held_idx;
ALTER TABLE conversations DROP COLUMN IF EXISTS held_at;
