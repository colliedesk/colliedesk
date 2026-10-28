-- Every number here is computed from conversations, messages, status changes and imported stats,
-- which stay; the next upgrade computes them again.
DROP TABLE results_members;
DROP TABLE results_snapshots;
DROP INDEX conversations_live_created;
ALTER TABLE results_state DROP COLUMN claimed_until;
