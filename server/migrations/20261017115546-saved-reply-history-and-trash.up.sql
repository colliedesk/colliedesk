-- Saved replies keep every version and go to a trash before they are deleted for good.
--
-- A deleted reply keeps its row with deleted_at set; it leaves the list and the reply box, can be
-- restored for 30 days, and the server purges it (with its history) after that. The title stays unique
-- only among replies outside the trash, so a deleted title can be used again; restoring then reports
-- the clash instead of hiding it.
--
-- saved_reply_versions is append-only for the app role: who made each version (the person, or the
-- person who approved an agent request), through which agent token, and the title and text it set.
-- Older images ignore the new columns and the new table; they list trashed replies as live.
ALTER TABLE saved_replies ADD COLUMN deleted_at timestamptz;
ALTER TABLE saved_replies ADD COLUMN deleted_by_agent_id uuid REFERENCES agents(id) ON DELETE SET NULL;
ALTER TABLE saved_replies ADD CONSTRAINT saved_replies_workspace_id_id_key UNIQUE (workspace_id, id);

DROP INDEX saved_replies_workspace_title_idx;
CREATE UNIQUE INDEX saved_replies_workspace_title_idx ON saved_replies(workspace_id, lower(title)) WHERE deleted_at IS NULL;
-- The daily purge finds replies deleted more than 30 days ago in every workspace.
CREATE INDEX saved_replies_trash_idx ON saved_replies(deleted_at) WHERE deleted_at IS NOT NULL;

CREATE TABLE saved_reply_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    saved_reply_id uuid NOT NULL,
    version integer NOT NULL CHECK (version >= 1),
    kind text NOT NULL CHECK (kind IN ('create', 'edit', 'delete', 'restore', 'undelete')),
    -- The version a restore copied; set only for kind restore.
    restored_from_version integer CHECK ((kind = 'restore') = (restored_from_version IS NOT NULL)),
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    body text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 20000),
    changed_by_agent_id uuid,
    -- The agent token that made or asked for the change. The label outlives the token.
    token_id uuid,
    token_label text,
    agent_request_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, saved_reply_id, version),
    CHECK ((token_id IS NULL) OR (token_label IS NOT NULL)),
    FOREIGN KEY (workspace_id, saved_reply_id) REFERENCES saved_replies(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, changed_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (changed_by_agent_id),
    FOREIGN KEY (workspace_id, token_id) REFERENCES sessions(workspace_id, id) ON DELETE SET NULL (token_id),
    FOREIGN KEY (workspace_id, agent_request_id) REFERENCES agent_requests(workspace_id, id) ON DELETE SET NULL (agent_request_id)
);

-- History starts here: each existing reply gets version 1 with the text it has now.
INSERT INTO saved_reply_versions(workspace_id, saved_reply_id, version, kind, title, body, changed_by_agent_id, created_at)
    SELECT workspace_id, id, 1, 'create', title, body, created_by_agent_id, updated_at FROM saved_replies;

-- A request that edits, deletes or restores a saved reply names it, and keeps the reply as the token
-- saw it when asking, so the person deciding sees a correct before and after even if the reply
-- changes meanwhile. The snapshot is cleared with the call 90 days after the decision.
ALTER TABLE agent_requests ADD COLUMN saved_reply_id uuid;
ALTER TABLE agent_requests ADD COLUMN saved_reply_before jsonb CHECK (saved_reply_before IS NULL OR octet_length(saved_reply_before::text) <= 131072);
ALTER TABLE agent_requests ADD CONSTRAINT agent_requests_saved_reply_fkey
    FOREIGN KEY (workspace_id, saved_reply_id) REFERENCES saved_replies(workspace_id, id) ON DELETE SET NULL (saved_reply_id);

-- Same isolation as saved_replies. The app role may read and add versions, never change or remove one.
DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE saved_reply_versions ENABLE ROW LEVEL SECURITY;
    ALTER TABLE saved_reply_versions FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON saved_reply_versions FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON saved_reply_versions FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT ON saved_reply_versions TO %I', app_role);
END
$$;
