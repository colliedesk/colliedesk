-- The place of an article among the articles of its collection, set by hand (PUT /api/v1/kb/order).
-- Lists sort by position, then title, then number, so 0 everywhere keeps the order every existing
-- article had (by title): no row is rewritten. Positions are not part of an article's versions.
ALTER TABLE kb_articles ADD COLUMN position integer NOT NULL DEFAULT 0 CHECK (position >= 0);
-- A collection's articles in that order: the admin list reads one page of them without sorting the
-- whole collection, and the help center numbers them per collection.
CREATE INDEX kb_articles_order_idx ON kb_articles(workspace_id, collection_id, position, lower(title), number) WHERE deleted_at IS NULL;
