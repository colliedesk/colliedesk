-- A complimentary paid plan granted by an operator: no payment, no trial clock. One row per grant
-- keeps the history (who, when, until when, who revoked); at most one grant is in force at a time.
-- Older images ignore this table, so rolling back to one only drops the comp plan, nothing else.
CREATE TABLE workspace_comp_plans (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    plan text NOT NULL CHECK (plan IN ('team')),
    granted_by text NOT NULL,
    granted_at timestamptz NOT NULL,
    -- NULL means forever.
    until timestamptz,
    revoked_by text,
    revoked_at timestamptz
);

CREATE UNIQUE INDEX workspace_comp_plans_in_force_idx ON workspace_comp_plans(workspace_id)
    WHERE revoked_at IS NULL;

-- Same isolation as workspace_billing.
SELECT secure_workspace_table('workspace_comp_plans');
