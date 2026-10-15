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

CREATE UNIQUE INDEX workspace_comp_plans_in_force_idx ON workspace_comp_plans(workspace_id) WHERE revoked_at IS NULL;

-- Same isolation as workspace_billing.
DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE workspace_comp_plans ENABLE ROW LEVEL SECURITY;
    ALTER TABLE workspace_comp_plans FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON workspace_comp_plans FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON workspace_comp_plans FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON workspace_comp_plans TO %I', app_role);
END
$$;
