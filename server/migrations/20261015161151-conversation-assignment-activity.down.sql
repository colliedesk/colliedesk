DROP INDEX IF EXISTS conversations_workspace_assignee_assigned_idx;
ALTER TABLE conversations
    DROP COLUMN IF EXISTS assigned_by_agent_id,
    DROP COLUMN IF EXISTS assigned_at;
