-- Whether an article's help center page lists its headings ("On this page"), in every language.
-- Versions keep it, so the history shows when it was turned off; restoring a version leaves it.
ALTER TABLE kb_articles ADD COLUMN show_contents boolean NOT NULL DEFAULT true;
ALTER TABLE kb_article_versions ADD COLUMN show_contents boolean NOT NULL DEFAULT true;
