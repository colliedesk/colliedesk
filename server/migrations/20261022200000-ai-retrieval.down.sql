-- Rolling back would forget which articles an admin kept away from AI and drop the versions that
-- say who did it, so it refuses while any exists. The retrieval rows are derived and go.
DO $$
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    IF EXISTS (SELECT 1 FROM kb_articles WHERE ai_excluded) OR EXISTS (SELECT 1 FROM kb_article_versions WHERE kind IN ('ai_exclude', 'ai_include')) THEN
        RAISE EXCEPTION 'AI retrieval rollback refused: % articles are excluded from AI and % versions record it',
            (SELECT count(*) FROM kb_articles WHERE ai_excluded), (SELECT count(*) FROM kb_article_versions WHERE kind IN ('ai_exclude', 'ai_include'));
    END IF;
END $$;

DROP TRIGGER ai_retrieval_message_added ON messages;
DROP TRIGGER ai_retrieval_conversation_inserted ON conversations;
DROP TRIGGER ai_retrieval_conversation_updated ON conversations;
DROP TRIGGER ai_retrieval_translation_inserted ON kb_article_translations;
DROP TRIGGER ai_retrieval_translation_updated ON kb_article_translations;
DROP TRIGGER ai_retrieval_article_inserted ON kb_articles;
DROP TRIGGER ai_retrieval_article_updated ON kb_articles;
DROP FUNCTION ai_retrieval_message_added();
DROP FUNCTION ai_retrieval_conversation_changed();
DROP FUNCTION ai_retrieval_translation_changed();
DROP FUNCTION ai_retrieval_article_changed();
DROP TABLE ai_retrieval_pending;
DROP TABLE ai_retrieval_docs;

ALTER TABLE kb_article_versions DROP CONSTRAINT kb_article_versions_kind_check;
ALTER TABLE kb_article_versions ADD CONSTRAINT kb_article_versions_kind_check
    CHECK (kind IN ('create', 'edit', 'delete', 'restore', 'undelete', 'import', 'media_replace'));
ALTER TABLE kb_articles DROP CONSTRAINT kb_articles_ai_excluded_by_fkey;
ALTER TABLE kb_articles DROP CONSTRAINT kb_articles_ai_excluded_at;
ALTER TABLE kb_articles DROP COLUMN ai_excluded_by_agent_id;
ALTER TABLE kb_articles DROP COLUMN ai_excluded_at;
ALTER TABLE kb_articles DROP COLUMN ai_excluded;
