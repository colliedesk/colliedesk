-- Uploads of large knowledge base media (videos) in parts. A row is one upload in progress: the size
-- announced, the bytes received so far, and when it expires. The parts go into a file of the
-- attachment storage's own disk (.incoming/media-<id>), so a completed upload is renamed into place,
-- never copied; the file is not named here. A cancelled upload loses its row; an expired
-- one goes with the daily purge. A completed upload keeps its row (completed_at, media_id) until it
-- expires, so a repeated complete answers the same item and the hourly limit of uploads per person can
-- count it. The open uploads reserve room: size - received counts against the plan and the disk when
-- the next one starts.
CREATE TABLE kb_media_uploads (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    kind text NOT NULL DEFAULT 'video' CHECK (kind IN ('video')),
    name text NOT NULL DEFAULT '' CHECK (char_length(name) <= 200),
    alt text NOT NULL DEFAULT '' CHECK (char_length(alt) <= 500),
    keep boolean NOT NULL DEFAULT false,
    size bigint NOT NULL CHECK (size > 0),
    received bigint NOT NULL DEFAULT 0 CHECK (received >= 0 AND received <= size),
    created_by_agent_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    completed_at timestamptz,
    media_id uuid,
    CHECK ((completed_at IS NULL) = (media_id IS NULL)),
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, created_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (created_by_agent_id)
);
-- The daily purge finds expired uploads in every workspace.
CREATE INDEX kb_media_uploads_expiry_idx ON kb_media_uploads(expires_at);

-- A video's poster is an image of any type the library takes; its type is kept with it.
ALTER TABLE kb_media ADD COLUMN poster_content_type text
    CHECK (poster_content_type IN ('image/png', 'image/jpeg', 'image/gif', 'image/webp'));
-- Posters so far came from the browser's frame grab, a JPEG.
UPDATE kb_media SET poster_content_type = 'image/jpeg' WHERE poster_storage_key IS NOT NULL;
ALTER TABLE kb_media ADD CONSTRAINT kb_media_poster_type_check CHECK ((poster_storage_key IS NULL) = (poster_content_type IS NULL));

DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE kb_media_uploads ENABLE ROW LEVEL SECURITY;
    ALTER TABLE kb_media_uploads FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON kb_media_uploads FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON kb_media_uploads FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON kb_media_uploads TO %I', app_role);
END
$$;
