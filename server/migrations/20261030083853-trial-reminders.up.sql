-- The trial-ending emails to a workspace's admins: one per admin, per kind, per trial end. A claim
-- is written before the email goes out, so two servers never send the same one; a send that failed
-- for a passing reason deletes its claim, so the next pass tries again. A trial end that moves (an
-- operator, a Freshdesk import) is a new trial end and gets reminders of its own. The table holds
-- agent ids, not addresses.
CREATE TABLE workspace_trial_reminders (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('three_days', 'end_day')),
    trial_ends_at timestamptz NOT NULL,
    status text NOT NULL CHECK (status IN ('claimed', 'sent', 'rejected')),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (workspace_id, agent_id, kind, trial_ends_at),
    FOREIGN KEY (workspace_id, agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE CASCADE
);

-- Same isolation as workspace_retention_notices.
SELECT secure_workspace_table('workspace_trial_reminders');
