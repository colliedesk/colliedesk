-- Dropping the column would let every deactivated agent sign in again, so the rollback refuses while
-- any agent is deactivated. Reactivate them (or remove them deliberately) first.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM agents WHERE deactivated_at IS NOT NULL) THEN
        RAISE EXCEPTION 'agent deactivation rollback refused: % deactivated agents would become active',
            (SELECT count(*) FROM agents WHERE deactivated_at IS NOT NULL);
    END IF;
END $$;

DROP INDEX IF EXISTS sessions_workspace_agent_seen_idx;
DROP INDEX IF EXISTS conversations_workspace_assignee_open_idx;
ALTER TABLE agents DROP COLUMN IF EXISTS deactivated_at;
