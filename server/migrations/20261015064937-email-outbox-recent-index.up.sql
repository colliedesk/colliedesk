-- The trial's daily reply cap counts a workspace's recent deliveries on every reply.
CREATE INDEX email_outbox_workspace_created_idx ON email_outbox(workspace_id, created_at);
