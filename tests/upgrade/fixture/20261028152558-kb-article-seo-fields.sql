-- The first workspace gave its password article its own search title and description, and the
-- version that saved them keeps them.
UPDATE kb_articles SET seo_title = 'Reset your Acme password', seo_description = 'Open Settings, then Password, to choose a new password.'
    WHERE id = md5('kba-acme-1')::uuid;
UPDATE kb_article_versions SET seo_title = 'Reset your Acme password', seo_description = 'Open Settings, then Password, to choose a new password.'
    WHERE article_id = md5('kba-acme-1')::uuid;
