-- Where a workspace's help center sends its page views: its own Google Analytics 4 property or
-- Plausible site. The GA4 API secret is sealed with MAILBOX_TOKEN_KEYS, bound to the workspace; the
-- fields of the provider not chosen are kept so switching back needs no retyping. last_sent_at,
-- last_error and last_error_at describe the sends since the last save, which clears them.
CREATE TABLE help_center_analytics (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    provider text NOT NULL CHECK (provider IN ('ga4', 'plausible')),
    enabled boolean NOT NULL DEFAULT false,
    ga4_measurement_id text,
    ga4_api_secret_sealed bytea,
    ga4_api_secret_key_id text,
    plausible_domain text,
    plausible_base_url text NOT NULL DEFAULT 'https://plausible.io',
    last_sent_at timestamptz,
    last_error text,
    last_error_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by_agent_id uuid,
    CHECK ((ga4_api_secret_sealed IS NULL) = (ga4_api_secret_key_id IS NULL)),
    FOREIGN KEY (workspace_id, updated_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (updated_by_agent_id)
);
CREATE INDEX help_center_analytics_enabled ON help_center_analytics(workspace_id) WHERE enabled;

DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE help_center_analytics ENABLE ROW LEVEL SECURITY;
    ALTER TABLE help_center_analytics FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON help_center_analytics FOR ALL TO %I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON help_center_analytics FOR ALL TO %I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON help_center_analytics TO %I', app_role);
END $$;
