-- Back to translations that are only kept and exported. Every translation row stays with its title and
-- text (those in the trash too); its slug, status, search fields, flags, versions, the help center's
-- languages and the collections' translated names go.
DROP TRIGGER ai_retrieval_translation_updated ON kb_article_translations;
CREATE OR REPLACE FUNCTION ai_retrieval_translation_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO ai_retrieval_pending(workspace_id, kb_article_id) VALUES (NEW.workspace_id, NEW.article_id);
    RETURN NULL;
END
$$;
CREATE TRIGGER ai_retrieval_translation_updated AFTER UPDATE OF title, body_md ON kb_article_translations
    FOR EACH ROW WHEN ((OLD.title, OLD.body_md) IS DISTINCT FROM (NEW.title, NEW.body_md)) EXECUTE FUNCTION ai_retrieval_translation_changed();
-- The older builder reads every translation: queue the articles that have one.
INSERT INTO ai_retrieval_pending(workspace_id, kb_article_id)
    SELECT DISTINCT t.workspace_id, t.article_id FROM kb_article_translations t
    JOIN kb_articles a ON a.workspace_id = t.workspace_id AND a.id = t.article_id
    WHERE a.status = 'published' AND a.deleted_at IS NULL;

DROP TABLE kb_collection_translations;
DROP TABLE kb_article_translation_versions;
DROP TABLE help_center_languages;
DROP INDEX kb_article_translations_locale_idx;
DROP INDEX kb_article_translations_trash_idx;
ALTER TABLE kb_article_translations
    DROP CONSTRAINT kb_article_translations_published_at,
    DROP CONSTRAINT kb_article_translations_outdated_at,
    DROP CONSTRAINT kb_article_translations_author_fkey,
    DROP CONSTRAINT kb_article_translations_deleted_by_fkey,
    DROP COLUMN search,
    DROP COLUMN slug,
    DROP COLUMN status,
    DROP COLUMN published_at,
    DROP COLUMN seo_title,
    DROP COLUMN seo_description,
    DROP COLUMN outdated,
    DROP COLUMN outdated_at,
    DROP COLUMN author_agent_id,
    DROP COLUMN deleted_at,
    DROP COLUMN deleted_by_agent_id;
