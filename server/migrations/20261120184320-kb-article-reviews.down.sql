-- Rolling back would drop every review, who was asked and who approved what, and the versions that
-- record them, so it refuses while any exists. Export the workspace first.
DO $$
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    IF EXISTS (SELECT 1 FROM kb_reviews)
        OR EXISTS (SELECT 1 FROM kb_article_versions WHERE kind LIKE 'review\_%')
        OR EXISTS (SELECT 1 FROM kb_article_translation_versions WHERE kind LIKE 'review\_%') THEN
        RAISE EXCEPTION 'knowledge base reviews rollback refused: % reviews exist and % versions record them',
            (SELECT count(*) FROM kb_reviews),
            (SELECT count(*) FROM kb_article_versions WHERE kind LIKE 'review\_%') + (SELECT count(*) FROM kb_article_translation_versions WHERE kind LIKE 'review\_%');
    END IF;
END $$;

ALTER TABLE kb_article_translation_versions DROP CONSTRAINT kb_article_translation_versions_kind_check;
ALTER TABLE kb_article_translation_versions ADD CONSTRAINT kb_article_translation_versions_kind_check
    CHECK (kind IN ('create', 'edit', 'delete', 'restore', 'undelete', 'import', 'media_replace', 'mark_current', 'mark_outdated', 'draft'));
ALTER TABLE kb_article_versions DROP CONSTRAINT kb_article_versions_kind_check;
ALTER TABLE kb_article_versions ADD CONSTRAINT kb_article_versions_kind_check
    CHECK (kind IN ('create', 'edit', 'delete', 'restore', 'undelete', 'import', 'media_replace', 'ai_exclude', 'ai_include', 'draft'));
DROP TABLE kb_review_reviewers;
DROP TABLE kb_reviews;
ALTER TABLE kb_collections DROP COLUMN require_approval;
ALTER TABLE help_centers DROP COLUMN require_approval;
