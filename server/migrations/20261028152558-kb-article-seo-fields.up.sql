-- An article's own title and description for search engines; empty uses the article's title and the
-- start of its text. Versions keep them, so restoring a version brings them back.
ALTER TABLE kb_articles
    ADD COLUMN seo_title text NOT NULL DEFAULT '' CHECK (char_length(seo_title) <= 120),
    ADD COLUMN seo_description text NOT NULL DEFAULT '' CHECK (char_length(seo_description) <= 320);
ALTER TABLE kb_article_versions
    ADD COLUMN seo_title text NOT NULL DEFAULT '',
    ADD COLUMN seo_description text NOT NULL DEFAULT '';
