-- Files attached to knowledge base articles: a PDF, an Office or OpenDocument file, CSV, text, RTF or
-- an archive a reader downloads from the help center. A file is a third kind of library item, so it
-- has the library's history, trash and undo. Its text (PDF, TXT, CSV) is read once at upload and kept
-- here, at most 200 KB, so the help center search and AI retrieval can find the article by it.
ALTER TABLE kb_media DROP CONSTRAINT kb_media_kind_check;
ALTER TABLE kb_media ADD CONSTRAINT kb_media_kind_check CHECK (kind IN ('image', 'video', 'file'));
ALTER TABLE kb_media DROP CONSTRAINT kb_media_content_type_check;
ALTER TABLE kb_media ADD CONSTRAINT kb_media_content_type_check CHECK (content_type IN (
    'image/png', 'image/jpeg', 'image/gif', 'image/webp', 'video/mp4', 'video/webm',
    'application/pdf', 'application/msword', 'application/vnd.ms-excel', 'application/vnd.ms-powerpoint',
    'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    'application/vnd.openxmlformats-officedocument.presentationml.presentation',
    'application/vnd.oasis.opendocument.text', 'application/vnd.oasis.opendocument.spreadsheet',
    'application/vnd.oasis.opendocument.presentation',
    'text/csv', 'text/plain', 'application/rtf', 'application/zip', 'application/x-7z-compressed', 'application/gzip'));
-- An image is an image type, a video a video type, a file anything else the list allows.
ALTER TABLE kb_media ADD CONSTRAINT kb_media_kind_check_file CHECK ((kind = 'file') = (content_type NOT LIKE 'image/%' AND content_type NOT LIKE 'video/%'));
ALTER TABLE kb_media ADD COLUMN text text CHECK (text IS NULL OR (kind = 'file' AND octet_length(text) <= 204800));
ALTER TABLE kb_media ADD COLUMN text_search tsvector GENERATED ALWAYS AS (kb_search_vector(COALESCE(text, ''))) STORED;

ALTER TABLE kb_media_uploads DROP CONSTRAINT kb_media_uploads_kind_check;
ALTER TABLE kb_media_uploads ADD CONSTRAINT kb_media_uploads_kind_check CHECK (kind IN ('video', 'file'));

-- A file replaced in place keeps its id, so every article and link keeps working: the new bytes move
-- into the replaced item and its old bytes into the other one, which goes to the trash as the earlier
-- version. Undo moves them back.
ALTER TABLE kb_media_replacements ADD COLUMN in_place boolean NOT NULL DEFAULT false;

-- The files an article lists under its body, in order: locale NULL is the article's own list, a
-- locale the list of that translation. A file listed by a published article (or published
-- translation) of a public help center is public; nothing else makes it so.
CREATE TABLE kb_article_attachments (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    article_id uuid NOT NULL,
    locale text,
    media_id uuid NOT NULL,
    position integer NOT NULL CHECK (position >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id, article_id) REFERENCES kb_articles(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, article_id, locale) REFERENCES kb_article_translations(workspace_id, article_id, locale) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, media_id) REFERENCES kb_media(workspace_id, id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX kb_article_attachments_item_idx ON kb_article_attachments(workspace_id, article_id, COALESCE(locale, ''), media_id);
-- The public download and the library's usage read the articles of one file.
CREATE INDEX kb_article_attachments_media_idx ON kb_article_attachments(workspace_id, media_id);

-- A published article's unpublished draft lists its own files; NULL keeps the live list.
ALTER TABLE kb_article_drafts ADD COLUMN attachment_ids uuid[];

DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE kb_article_attachments ENABLE ROW LEVEL SECURITY;
    ALTER TABLE kb_article_attachments FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON kb_article_attachments FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON kb_article_attachments FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON kb_article_attachments TO %I', app_role);
END
$$;

-- AI retrieval reads an article's files with its text: a list that changes, or a file whose text or
-- name changes (a replace in place), queues the articles that list it.
CREATE FUNCTION ai_retrieval_attachment_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO ai_retrieval_pending(workspace_id, kb_article_id)
        SELECT DISTINCT workspace_id, article_id FROM changed;
    RETURN NULL;
END
$$;
CREATE TRIGGER ai_retrieval_attachment_inserted AFTER INSERT ON kb_article_attachments
    REFERENCING NEW TABLE AS changed FOR EACH STATEMENT EXECUTE FUNCTION ai_retrieval_attachment_changed();
CREATE TRIGGER ai_retrieval_attachment_deleted AFTER DELETE ON kb_article_attachments
    REFERENCING OLD TABLE AS changed FOR EACH STATEMENT EXECUTE FUNCTION ai_retrieval_attachment_changed();

CREATE FUNCTION ai_retrieval_file_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO ai_retrieval_pending(workspace_id, kb_article_id)
        SELECT DISTINCT workspace_id, article_id FROM kb_article_attachments
        WHERE workspace_id = NEW.workspace_id AND media_id = NEW.id;
    RETURN NULL;
END
$$;
CREATE TRIGGER ai_retrieval_file_updated AFTER UPDATE OF text, name ON kb_media
    FOR EACH ROW WHEN (NEW.kind = 'file' AND (OLD.text, OLD.name) IS DISTINCT FROM (NEW.text, NEW.name))
    EXECUTE FUNCTION ai_retrieval_file_changed();
