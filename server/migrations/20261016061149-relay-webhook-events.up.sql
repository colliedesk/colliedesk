-- Relay webhook events (Resend bounces and complaints) already applied, so a redelivery of the same
-- event changes nothing, even after an agent retried the reply it failed.
CREATE TABLE relay_webhook_events (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    webhook_id text NOT NULL,
    event_type text NOT NULL,
    message_id uuid NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, webhook_id),
    FOREIGN KEY (workspace_id, message_id) REFERENCES messages(workspace_id, id) ON DELETE CASCADE
);

-- Same isolation as the tables of the row-level security migration; the app role comes from the
-- policy on workspaces, so this migration needs no role name of its own.
DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE relay_webhook_events ENABLE ROW LEVEL SECURITY;
    ALTER TABLE relay_webhook_events FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON relay_webhook_events FOR ALL TO %I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$,
        app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON relay_webhook_events FOR ALL TO %I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$,
        current_user);
    -- A table made by a role other than the one that ran the row-level security migration does not
    -- inherit its default privileges, so grant them here.
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON relay_webhook_events TO %I', app_role);
END
$$;
