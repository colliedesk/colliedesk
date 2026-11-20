-- Rolling back would drop the text of every unpublished draft and the versions that record it, so it
-- refuses while any exists. Publish or discard them first, or export the workspace.
DO $$
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    IF EXISTS (SELECT 1 FROM kb_article_drafts)
        OR EXISTS (SELECT 1 FROM kb_article_versions WHERE kind = 'draft')
        OR EXISTS (SELECT 1 FROM kb_article_translation_versions WHERE kind = 'draft') THEN
        RAISE EXCEPTION 'knowledge base drafts rollback refused: % articles or translations have unpublished changes and % versions record them',
            (SELECT count(*) FROM kb_article_drafts),
            (SELECT count(*) FROM kb_article_versions WHERE kind = 'draft') + (SELECT count(*) FROM kb_article_translation_versions WHERE kind = 'draft');
    END IF;
END $$;

ALTER TABLE kb_article_translation_versions DROP CONSTRAINT kb_article_translation_versions_kind_check;
ALTER TABLE kb_article_translation_versions ADD CONSTRAINT kb_article_translation_versions_kind_check
    CHECK (kind IN ('create', 'edit', 'delete', 'restore', 'undelete', 'import', 'media_replace', 'mark_current', 'mark_outdated'));
ALTER TABLE kb_article_versions DROP CONSTRAINT kb_article_versions_kind_check;
ALTER TABLE kb_article_versions ADD CONSTRAINT kb_article_versions_kind_check
    CHECK (kind IN ('create', 'edit', 'delete', 'restore', 'undelete', 'import', 'media_replace', 'ai_exclude', 'ai_include'));
DROP TABLE kb_article_drafts;
