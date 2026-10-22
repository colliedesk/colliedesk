-- An older image orders the articles of a collection by title again; the order set by hand is lost.
DROP INDEX IF EXISTS kb_articles_order_idx;
ALTER TABLE kb_articles DROP COLUMN IF EXISTS position;
