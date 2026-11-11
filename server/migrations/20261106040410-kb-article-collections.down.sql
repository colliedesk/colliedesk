-- Back to one collection per article: every article keeps its home (kb_articles.collection_id); the
-- other collections it was listed in are dropped. Export first to keep them.
DROP TABLE kb_article_collections;
