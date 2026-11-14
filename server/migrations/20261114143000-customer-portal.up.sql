-- The customer portal on the help center (docs/user/customer-portal.md): an admin turns it on, and a
-- requester signs in with a one-time email link to see only their own requests. The link and the
-- session are stored as hashes, like a pairing link. Turning the portal off does not delete them.
-- portal is an origin of conversations, like agent and problem_report: nothing connects to it.
INSERT INTO channel_types (type, connectable) VALUES ('portal', false);
ALTER TABLE help_centers ADD COLUMN portal_enabled boolean NOT NULL DEFAULT false;

CREATE TABLE portal_links (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    email text NOT NULL CHECK (email = lower(email) AND char_length(email) BETWEEN 3 AND 254),
    token_hash bytea NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at timestamptz
);
CREATE INDEX portal_links_expiry ON portal_links(workspace_id, expires_at);

CREATE TABLE portal_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    email text NOT NULL CHECK (email = lower(email) AND char_length(email) BETWEEN 3 AND 254),
    token_hash bytea NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz
);
CREATE INDEX portal_sessions_live ON portal_sessions(token_hash) WHERE revoked_at IS NULL;

DO $$
DECLARE
    app_role text;
    table_name text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    FOREACH table_name IN ARRAY ARRAY['portal_links', 'portal_sessions'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', table_name);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', table_name);
        EXECUTE format($policy$CREATE POLICY workspace_isolation ON %2$I FOR ALL TO %1$I
            USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
            WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role, table_name);
        EXECUTE format($policy$CREATE POLICY privileged_access ON %2$I FOR ALL TO %1$I
            USING ((SELECT current_setting('app.privileged', true) = 'on'))
            WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user, table_name);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON %I TO %I', table_name, app_role);
    END LOOP;
END
$$;
