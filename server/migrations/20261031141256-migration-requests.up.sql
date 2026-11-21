-- The Freshdesk data moves an admin asked Colliedesk to do. The email to support is the request
-- itself; this row is the admin's proof that it went out, and what Plan and billing shows
-- afterwards. The row stays when the admin who asked is removed. It holds the Freshdesk host name
-- and the times the admin typed, no ticket data.
CREATE TABLE workspace_migration_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    requested_by uuid,
    freshdesk_address text NOT NULL CHECK (freshdesk_address <> ''),
    times text NOT NULL DEFAULT '',
    requested_at timestamptz NOT NULL,
    FOREIGN KEY (workspace_id, requested_by)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (requested_by)
);
CREATE INDEX workspace_migration_requests_workspace_time
    ON workspace_migration_requests (workspace_id, requested_at DESC);

SELECT secure_workspace_table('workspace_migration_requests');
