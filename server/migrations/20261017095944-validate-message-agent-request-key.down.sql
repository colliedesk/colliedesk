-- Validation has nothing to undo: the key stays, and the down of the migration that added it drops the key
-- with its column. The index this migration added goes.
DROP INDEX IF EXISTS messages_agent_request_idx;
