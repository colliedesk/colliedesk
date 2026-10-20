-- Rolling back would delete the knowledge base people wrote, so it refuses while any collection,
-- article or image exists. Export the workspace and delete them deliberately first.
DO $$
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    IF EXISTS (SELECT 1 FROM kb_collections) OR EXISTS (SELECT 1 FROM kb_articles) OR EXISTS (SELECT 1 FROM kb_images) THEN
        RAISE EXCEPTION 'knowledge base rollback refused: % collections, % articles and % images would be deleted',
            (SELECT count(*) FROM kb_collections), (SELECT count(*) FROM kb_articles), (SELECT count(*) FROM kb_images);
    END IF;
END $$;

DROP TABLE IF EXISTS help_centers;
DROP TABLE IF EXISTS kb_article_images;
DROP TABLE IF EXISTS kb_images;
DROP FUNCTION IF EXISTS kb_images_count_inserted();
DROP FUNCTION IF EXISTS kb_images_count_deleted();
DROP TABLE IF EXISTS kb_article_translations;
DROP TABLE IF EXISTS kb_article_versions;
DROP TABLE IF EXISTS kb_articles;
DROP TABLE IF EXISTS kb_collections;
