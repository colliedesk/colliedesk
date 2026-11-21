-- Retention of locked workspaces.
--
-- retention_notice_sent_at: when the last admin of a locked workspace was told it will be deleted.
-- The 30 day clock starts then. Cleared when the workspace gets access again.
ALTER TABLE workspaces ADD COLUMN retention_notice_sent_at timestamptz;

-- Which admins were already told (or whose address the relay refused for good) in the current
-- lock, so a partly failed pass never mails the same admin twice. It holds agent ids, not
-- addresses.
CREATE TABLE workspace_retention_notices (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id uuid NOT NULL,
    status text NOT NULL CHECK (status IN ('sent', 'rejected')),
    sent_at timestamptz NOT NULL,
    PRIMARY KEY (workspace_id, agent_id),
    FOREIGN KEY (workspace_id, agent_id) REFERENCES agents(workspace_id, id) ON DELETE CASCADE
);

-- One row per deleted workspace: the audit trail, and what the daily deletion cap counts. Platform
-- level and without personal data; it outlives the workspace on purpose.
CREATE TABLE retention_deletions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    deleted_workspace_id uuid NOT NULL,
    slug text NOT NULL,
    deleted_at timestamptz NOT NULL
);

CREATE INDEX retention_deletions_deleted_at_idx ON retention_deletions(deleted_at);

-- Same isolation as workspace_comp_plans.
SELECT secure_workspace_table('workspace_retention_notices');

-- Same isolation as cloud_signups: only the privileged retention transaction reads it.
SELECT secure_server_table('retention_deletions');
