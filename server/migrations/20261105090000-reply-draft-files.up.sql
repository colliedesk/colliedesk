-- The files a written reply draft carries (POST /conversations/{id}/reply-drafts with files). Their
-- bytes sit in attachment storage like a message's; a row here references them, so DiscardObject keeps
-- them while the draft holds them. They count toward the workspace's storage while the row exists.
-- Sending the draft copies the files a person kept to the reply's attachments and removes these rows
-- in the same transaction; a sent or stopped draft's rows go, and an ended draft's rows go with its text
-- (90 days), so an earlier draft (replaced, expired or discarded) can be brought back with its files.
CREATE TABLE ai_draft_files (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    draft_id uuid NOT NULL,
    filename text NOT NULL CHECK (btrim(filename) <> '' AND char_length(filename) <= 255),
    content_type text NOT NULL CHECK (char_length(content_type) BETWEEN 1 AND 255),
    size bigint NOT NULL CHECK (size >= 0),
    sha256 text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    storage_key text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, draft_id) REFERENCES ai_drafts(workspace_id, id) ON DELETE CASCADE
);
CREATE INDEX ai_draft_files_draft_idx ON ai_draft_files(workspace_id, draft_id, created_at);
CREATE INDEX ai_draft_files_object_idx ON ai_draft_files(workspace_id, storage_key);

-- Who brought an earlier written draft back, and when: the draft row itself waits again.
ALTER TABLE ai_drafts ADD COLUMN restored_at timestamptz;
ALTER TABLE ai_drafts ADD COLUMN restored_by_agent_id uuid;

CREATE FUNCTION ai_draft_files_count() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    delta bigint := 0;
    workspace uuid;
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        delta := delta - OLD.size;
        workspace := OLD.workspace_id;
    END IF;
    IF TG_OP IN ('INSERT', 'UPDATE') THEN
        delta := delta + NEW.size;
        workspace := NEW.workspace_id;
    END IF;
    IF delta <> 0 THEN
        UPDATE workspaces SET counted_storage_bytes = counted_storage_bytes + delta, stored_storage_bytes = stored_storage_bytes + delta
            WHERE id = workspace;
    END IF;
    RETURN NULL;
END
$$;
CREATE TRIGGER ai_draft_files_count AFTER INSERT OR UPDATE OF size OR DELETE ON ai_draft_files
    FOR EACH ROW EXECUTE FUNCTION ai_draft_files_count();

DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE ai_draft_files ENABLE ROW LEVEL SECURITY;
    ALTER TABLE ai_draft_files FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON ai_draft_files FOR ALL TO %I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON ai_draft_files FOR ALL TO %I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON ai_draft_files TO %I', app_role);
END
$$;

-- What a reply sent from a draft took from it (the draft, the files asked for, the files that went),
-- with its Idempotency-Key: a repeat that asks for other files is another message (422).
ALTER TABLE message_idempotency_keys ADD COLUMN draft_selection jsonb;
