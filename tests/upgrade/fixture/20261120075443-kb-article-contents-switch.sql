-- The first workspace turned off the list of contents of its password article, and its newest
-- version keeps that.
UPDATE kb_articles SET show_contents = false WHERE id = md5('kba-acme-1')::uuid;
UPDATE kb_article_versions SET show_contents = false
    WHERE article_id = md5('kba-acme-1')::uuid
      AND version = (SELECT max(version) FROM kb_article_versions WHERE article_id = md5('kba-acme-1')::uuid);
