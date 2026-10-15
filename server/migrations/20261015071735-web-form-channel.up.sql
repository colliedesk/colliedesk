-- The web form channel: a public website form opens a conversation in the workspace. One row per
-- workspace; no row means the form is off. Off is the default, and a disabled form answers like an
-- unknown workspace. Older images ignore this table.
CREATE TABLE web_form_channels (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    enabled boolean NOT NULL DEFAULT false,
    -- Exact origins (scheme://host[:port]) the browser may call the form from.
    allowed_origins text[] NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

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
    ALTER TABLE web_form_channels ENABLE ROW LEVEL SECURITY;
    ALTER TABLE web_form_channels FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON web_form_channels FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON web_form_channels FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON web_form_channels TO %I', app_role);
END
$$;
