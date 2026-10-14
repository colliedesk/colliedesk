CREATE INDEX conversations_workspace_status_updated_idx
    ON conversations(workspace_id, status, updated_at DESC, id DESC);
