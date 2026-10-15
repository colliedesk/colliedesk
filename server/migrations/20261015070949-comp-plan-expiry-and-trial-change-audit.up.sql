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

CREATE INDEX workspace_trial_changes_workspace_idx ON workspace_trial_changes(workspace_id, changed_at);

-- Same isolation as workspace_comp_plans.
DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE workspace_trial_changes ENABLE ROW LEVEL SECURITY;
    ALTER TABLE workspace_trial_changes FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON workspace_trial_changes FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON workspace_trial_changes FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON workspace_trial_changes TO %I', app_role);
END
$$;
