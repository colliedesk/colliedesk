-- Remote images of incoming email, fetched once by the image proxy and kept in attachment storage so
-- a second view does not call the third party. Rows only point at stored bytes (storage_key); a row is
-- derived data that the proxy fetches again when it is missing or older than its lifetime. The key is
-- per workspace, so one workspace cannot learn what another has viewed. Older images ignore this table.
CREATE TABLE mail_image_cache (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    -- SHA-256 (hex) of the original URL.
    url_hash text NOT NULL CHECK (url_hash ~ '^[0-9a-f]{64}$'),
    storage_key text NOT NULL,
    content_type text NOT NULL CHECK (content_type IN ('image/png', 'image/jpeg', 'image/gif', 'image/webp')),
    size bigint NOT NULL CHECK (size > 0),
    fetched_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, url_hash)
);

-- Deleting an attachment object checks whether the cache still points at it.
CREATE INDEX mail_image_cache_storage_key_idx ON mail_image_cache(workspace_id, storage_key);

-- Same isolation as agent_requests.
DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE mail_image_cache ENABLE ROW LEVEL SECURITY;
    ALTER TABLE mail_image_cache FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON mail_image_cache FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON mail_image_cache FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON mail_image_cache TO %I', app_role);
END
$$;
