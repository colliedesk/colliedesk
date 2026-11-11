-- The trial-ending emails to a workspace's admins: one per admin, per kind, per trial end. A claim is
-- written before the email goes out, so two servers never send the same one; a send that failed for a
-- passing reason deletes its claim, so the next pass tries again. A trial end that moves (an operator, a
-- Freshdesk import) is a new trial end and gets reminders of its own. The table holds agent ids, not addresses.
CREATE TABLE workspace_trial_reminders (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('three_days', 'end_day')),
    trial_ends_at timestamptz NOT NULL,
    status text NOT NULL CHECK (status IN ('claimed', 'sent', 'rejected')),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (workspace_id, agent_id, kind, trial_ends_at),
    FOREIGN KEY (workspace_id, agent_id) REFERENCES agents(workspace_id, id) ON DELETE CASCADE
);

DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    -- Same isolation as workspace_retention_notices.
    ALTER TABLE workspace_trial_reminders ENABLE ROW LEVEL SECURITY;
    ALTER TABLE workspace_trial_reminders FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON workspace_trial_reminders FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON workspace_trial_reminders FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON workspace_trial_reminders TO %I', app_role);
END
$$;
