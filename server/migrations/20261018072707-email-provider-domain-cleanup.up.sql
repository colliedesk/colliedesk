-- Provider domains a workspace no longer uses (its support domain changed or the channel was
-- disconnected) that still have to be deleted at the email provider. The id is written in the same
-- transaction that drops it from the channel, and removed only after the provider confirms the delete,
-- so a failed call is retried on the workspace's next save, verify or disconnect.
CREATE TABLE email_provider_domain_trash (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    provider_domain_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, provider_domain_id)
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
    ALTER TABLE email_provider_domain_trash ENABLE ROW LEVEL SECURITY;
    ALTER TABLE email_provider_domain_trash FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON email_provider_domain_trash FOR ALL TO %I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$,
        app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON email_provider_domain_trash FOR ALL TO %I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$,
        current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON email_provider_domain_trash TO %I', app_role);
END
$$;
