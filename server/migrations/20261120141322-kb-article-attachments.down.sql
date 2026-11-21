-- Rolling back would lose every file of the library and the lists that attach them, so it refuses
-- while any file or file upload exists. Purge them first, or export the workspace.
DO $$
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    IF EXISTS (SELECT 1 FROM kb_media WHERE kind = 'file')
        OR EXISTS (SELECT 1 FROM kb_media_uploads WHERE kind = 'file') THEN
        RAISE EXCEPTION 'knowledge base attachments rollback refused: % files and % file uploads '
            'exist',
            (SELECT count(*) FROM kb_media WHERE kind = 'file'),
            (SELECT count(*) FROM kb_media_uploads WHERE kind = 'file');
    END IF;
END $$;

DROP TRIGGER ai_retrieval_file_updated ON kb_media;
DROP FUNCTION ai_retrieval_file_changed();
DROP TABLE kb_article_attachments;
DROP FUNCTION ai_retrieval_attachment_changed();
ALTER TABLE kb_article_drafts DROP COLUMN attachment_ids;
ALTER TABLE kb_media_replacements DROP COLUMN in_place;
ALTER TABLE kb_media_uploads DROP CONSTRAINT kb_media_uploads_kind_check;
ALTER TABLE kb_media_uploads ADD CONSTRAINT kb_media_uploads_kind_check
    CHECK (kind IN ('video'));
ALTER TABLE kb_media DROP COLUMN text_search;
ALTER TABLE kb_media DROP COLUMN text;
ALTER TABLE kb_media DROP CONSTRAINT kb_media_kind_check_file;
ALTER TABLE kb_media DROP CONSTRAINT kb_media_content_type_check;
ALTER TABLE kb_media ADD CONSTRAINT kb_media_content_type_check
    CHECK (content_type IN (
        'image/png', 'image/jpeg', 'image/gif', 'image/webp', 'video/mp4', 'video/webm'
    ));
ALTER TABLE kb_media DROP CONSTRAINT kb_media_kind_check;
ALTER TABLE kb_media ADD CONSTRAINT kb_media_kind_check CHECK (kind IN ('image', 'video'));
