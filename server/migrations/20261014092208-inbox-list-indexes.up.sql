-- Serves the inbox list, which filters a workspace's conversations by status and shows the most
-- recently updated first.
CREATE INDEX conversations_workspace_status_updated_idx
    ON conversations(workspace_id, status, updated_at DESC, id DESC);
