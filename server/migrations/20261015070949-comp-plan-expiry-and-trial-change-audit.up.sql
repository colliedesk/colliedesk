-- Why a complimentary grant stopped: ended by an operator, replaced by a newer grant, or past its
-- end date. Rows written before this column keep NULL.
ALTER TABLE workspace_comp_plans
    ADD COLUMN revoke_reason text CHECK (revoke_reason IN ('revoked', 'replaced', 'expired'));

-- Every operator change of a trial end: who, when, from what to what.
CREATE TABLE workspace_trial_changes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    previous_ends_at timestamptz NOT NULL,
    new_ends_at timestamptz NOT NULL,
    changed_by text NOT NULL,
    changed_at timestamptz NOT NULL
);

CREATE INDEX workspace_trial_changes_workspace_idx
    ON workspace_trial_changes(workspace_id, changed_at);

-- Same isolation as workspace_comp_plans.
SELECT secure_workspace_table('workspace_trial_changes');
