-- Saved replies: text the whole workspace shares and any agent inserts into the reply box.
-- The title is unique per workspace ignoring case; the limits on length and count are also
-- enforced by the API. Older images ignore this table.
CREATE TABLE saved_replies (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    body text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 20000),
    created_by_agent_id uuid REFERENCES agents(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX saved_replies_workspace_title_idx ON saved_replies(workspace_id, lower(title));

-- Same isolation as web_form_channels.
DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE saved_replies ENABLE ROW LEVEL SECURITY;
    ALTER TABLE saved_replies FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON saved_replies FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON saved_replies FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON saved_replies TO %I', app_role);
END
$$;
