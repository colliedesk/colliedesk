-- Relay webhook events (Resend bounces and complaints) already applied, so a redelivery of the
-- same event changes nothing, even after an agent retried the reply it failed.
CREATE TABLE relay_webhook_events (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    webhook_id text NOT NULL,
    event_type text NOT NULL,
    message_id uuid NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, webhook_id),
    FOREIGN KEY (workspace_id, message_id) REFERENCES messages(workspace_id, id) ON DELETE CASCADE
);

-- Same isolation as the tables of the row-level security migration.
SELECT secure_workspace_table('relay_webhook_events');
