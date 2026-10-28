-- The history is lost with the table: conversations keep their status, and the next upgrade starts a
-- new history (status_history_since is set again), so the results page never reads the gap. Take a
-- backup before rolling back (docs/self-host/upgrade.md).
DROP TRIGGER conversation_status_changes_record ON conversations;
DROP FUNCTION conversation_status_changes_record();
DROP TABLE conversation_status_changes;
ALTER TABLE workspaces DROP COLUMN status_history_since;
