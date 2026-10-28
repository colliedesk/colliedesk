-- The custom domain of a workspace's help center, such as help.example.com: one per workspace. A
-- host name is pending until a DNS check proves the workspace owns it (a CNAME to its own
-- <slug>.<HELP_CENTER_DOMAIN> or the token in a TXT record), then active. Several workspaces may hold
-- the same host name while it is pending; the first to be verified wins it, and the partial unique
-- index keeps a second from becoming active. The server stores the host name in lower case ASCII
-- (punycode) and the token as 32 hex digits; the checks keep any other writer to the same shapes.
CREATE TABLE help_center_domains (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    hostname text NOT NULL CHECK (char_length(hostname) BETWEEN 3 AND 253 AND hostname = lower(hostname)),
    token text NOT NULL CHECK (token ~ '^[0-9a-f]{32}$'),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'active')),
    created_at timestamptz NOT NULL DEFAULT now(),
    verified_at timestamptz,
    checked_at timestamptz,
    check_error text CHECK (char_length(check_error) <= 500),
    certificate_requested_at timestamptz,
    CHECK (status = 'pending' OR verified_at IS NOT NULL)
);

-- Serving and the proxy's certificate question find the active workspace of a host name; the
-- certificate question also finds the pending claims of one.
CREATE UNIQUE INDEX help_center_domains_active_hostname ON help_center_domains (lower(hostname)) WHERE status = 'active';
CREATE INDEX help_center_domains_hostname ON help_center_domains (lower(hostname));

-- Same isolation as help_centers.
DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE help_center_domains ENABLE ROW LEVEL SECURITY;
    ALTER TABLE help_center_domains FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON help_center_domains FOR ALL TO %I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON help_center_domains FOR ALL TO %I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON help_center_domains TO %I', app_role);
END
$$;
