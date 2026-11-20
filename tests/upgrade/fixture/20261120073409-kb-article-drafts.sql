-- acme holds unpublished changes to its published password article (moved to Getting started, listed
-- in Accounts too, On this page turned off) and to its German translation; the live rows stay as they were. Each save is a
-- version of kind draft.
INSERT INTO kb_article_drafts (id, workspace_id, article_id, locale, title, slug, body_md, seo_title, seo_description, collection_id, extra_collection_ids,
        show_contents, updated_by_agent_id, created_at, updated_at) VALUES
    (md5('kbd-acme-1')::uuid, md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, NULL, 'Reset or change your password', 'reset-or-change-your-password',
     E'Open **Settings**, then *Security*.', 'Password help', 'Reset or change it', md5('kbc-acme-1')::uuid, ARRAY[md5('kbc-acme-2')::uuid],
     false, md5('agent-acme-ben')::uuid, '2026-11-20 07:00:00+00', '2026-11-20 07:10:00+00'),
    (md5('kbd-acme-1-de')::uuid, md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, 'de', 'Passwort ändern', 'passwort-ändern',
     'Öffnen Sie **Sicherheit**.', '', '', NULL, NULL, NULL, md5('agent-acme-ben')::uuid, '2026-11-20 07:00:00+00', '2026-11-20 07:05:00+00');

INSERT INTO kb_article_versions (id, workspace_id, article_id, version, kind, title, slug, body_md, status, collection_id, changed_by_agent_id, created_at) VALUES
    (md5('kba-acme-1-v-draft')::uuid, md5('ws-acme')::uuid, md5('kba-acme-1')::uuid,
     (SELECT max(version) + 1 FROM kb_article_versions WHERE article_id = md5('kba-acme-1')::uuid), 'draft',
     'Reset or change your password', 'reset-or-change-your-password', E'Open **Settings**, then *Security*.', 'published', md5('kbc-acme-1')::uuid,
     md5('agent-acme-ben')::uuid, '2026-11-20 07:10:00+00');
INSERT INTO kb_article_translation_versions (id, workspace_id, article_id, locale, version, kind, title, slug, body_md, status, changed_by_agent_id, created_at) VALUES
    (md5('kbt-acme-de-v-draft')::uuid, md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, 'de',
     (SELECT max(version) + 1 FROM kb_article_translation_versions WHERE article_id = md5('kba-acme-1')::uuid AND locale = 'de'), 'draft',
     'Passwort ändern', 'passwort-ändern', 'Öffnen Sie **Sicherheit**.', 'published', md5('agent-acme-ben')::uuid, '2026-11-20 07:05:00+00');
