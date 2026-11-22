-- Rolling back would drop the versions that record who an article was credited to before, so it
-- refuses while any exists. Export the workspace first.
DO $$
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    IF EXISTS (SELECT 1 FROM kb_article_versions WHERE kind = 'author_change') THEN
        RAISE EXCEPTION
            'knowledge base author change rollback refused: % versions record an author change',
            (SELECT count(*) FROM kb_article_versions WHERE kind = 'author_change');
    END IF;
END $$;

ALTER TABLE kb_article_versions DROP CONSTRAINT kb_article_versions_kind_check;
ALTER TABLE kb_article_versions ADD CONSTRAINT kb_article_versions_kind_check
    CHECK (kind IN ('create', 'edit', 'delete', 'restore', 'undelete', 'import', 'media_replace',
        'ai_exclude', 'ai_include', 'draft',
        'review_request', 'review_approve', 'review_changes', 'review_withdraw'));

ALTER TABLE kb_article_versions
    DROP COLUMN author_from_agent_id,
    DROP COLUMN author_to_agent_id;
