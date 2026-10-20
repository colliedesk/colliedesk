-- Rolling back would forget which articles a replacement changed and drop the versions it made, so it
-- refuses while any replacement or media_replace version exists. Trash batches only serve Undo.
DO $$
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    IF EXISTS (SELECT 1 FROM kb_media_replacements) OR EXISTS (SELECT 1 FROM kb_article_versions WHERE kind = 'media_replace') THEN
        RAISE EXCEPTION 'knowledge base media replacement rollback refused: % replacements and % versions would be lost',
            (SELECT count(*) FROM kb_media_replacements), (SELECT count(*) FROM kb_article_versions WHERE kind = 'media_replace');
    END IF;
END $$;

ALTER TABLE kb_article_versions DROP CONSTRAINT kb_article_versions_kind_check;
ALTER TABLE kb_article_versions ADD CONSTRAINT kb_article_versions_kind_check
    CHECK (kind IN ('create', 'edit', 'delete', 'restore', 'undelete', 'import'));
DROP TABLE kb_media_replacements;
DROP TABLE kb_media_batches;
