-- Rolling back would lose every article tag, published or in a draft, so it refuses while any exists.
-- Remove them first, or export the workspace.
DO $$
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    IF EXISTS (SELECT 1 FROM kb_article_tags) OR EXISTS (SELECT 1 FROM kb_article_drafts WHERE cardinality(tags) > 0) THEN
        RAISE EXCEPTION 'knowledge base article tags rollback refused: % tags exist', (SELECT count(*) FROM kb_article_tags);
    END IF;
END $$;

DROP TABLE kb_article_tags;
DROP FUNCTION ai_retrieval_tag_changed();
ALTER TABLE kb_article_drafts DROP COLUMN tags;
ALTER TABLE help_centers DROP COLUMN show_tags;
