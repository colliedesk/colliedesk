-- Rolling back would put every article kept for help chat or the team on the help center, so
-- it refuses while any article or collection is not visible to everyone. Set them back to
-- everyone first.
DO $$
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    IF EXISTS (SELECT 1 FROM kb_articles WHERE visibility <> 'everyone')
        OR EXISTS (SELECT 1 FROM kb_collections WHERE visibility <> 'everyone') THEN
        RAISE EXCEPTION 'knowledge base visibility rollback refused: % articles and % collections '
            'are not visible to everyone',
            (SELECT count(*) FROM kb_articles WHERE visibility <> 'everyone'),
            (SELECT count(*) FROM kb_collections WHERE visibility <> 'everyone');
    END IF;
END $$;

DROP VIEW kb_public_translations;
DROP VIEW kb_help_chat_translations;
DROP VIEW kb_published_translations;
DROP VIEW kb_public_articles;
DROP VIEW kb_help_chat_articles;
DROP VIEW kb_published_articles;
DROP VIEW kb_article_visibility;
DROP VIEW kb_article_collection_ranks;
DROP VIEW kb_collection_visibility;
DROP FUNCTION kb_visibility_name(integer);
DROP FUNCTION kb_visibility_rank(text);
ALTER TABLE kb_collections DROP COLUMN visibility;
ALTER TABLE kb_articles DROP COLUMN visibility;
