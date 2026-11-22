-- Records when a conversation was assigned and by whom, so the activity view can show assignments
-- over time. The index serves that view: a workspace's conversations per assignee, newest
-- assignment first.
ALTER TABLE conversations
    ADD COLUMN assigned_at timestamptz,
    ADD COLUMN assigned_by_agent_id uuid,
    ADD FOREIGN KEY (workspace_id, assigned_by_agent_id) REFERENCES agents(workspace_id, id)
        ON DELETE SET NULL (assigned_by_agent_id);

CREATE INDEX conversations_workspace_assignee_assigned_idx
    ON conversations(workspace_id, assignee_id, assigned_at DESC)
    WHERE assigned_at IS NOT NULL;
