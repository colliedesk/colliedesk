-- A deactivated agent keeps its row (messages, notes and old assignments still name it) but cannot
-- sign in and takes no seat. NULL means active.
ALTER TABLE agents ADD COLUMN deactivated_at timestamptz;

-- Deactivating counts and moves the open and pending conversations of one agent.
CREATE INDEX conversations_workspace_assignee_open_idx
    ON conversations(workspace_id, assignee_id)
    WHERE assignee_id IS NOT NULL AND status IN ('open', 'pending');

-- The team list shows when each agent was last seen.
CREATE INDEX sessions_workspace_agent_seen_idx ON sessions(workspace_id, agent_id, last_seen_at DESC);
