-- acme's password article was viewed on two days in English and once in German; beta's on one day.
-- An acme reply linked the password article and the same reply used it as an AI draft source.
INSERT INTO kb_article_views (workspace_id, kb_article_id, locale, day, views) VALUES
    (md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, 'en', '2026-11-20', 42),
    (md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, 'en', '2026-11-21', 7),
    (md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, 'de', '2026-11-21', 3),
    (md5('ws-beta')::uuid, md5('kba-beta-1')::uuid, 'en', '2026-11-21', 1);

INSERT INTO kb_article_uses (workspace_id, kb_article_id, conversation_id, message_id, kind, created_at) VALUES
    (md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, md5('conv-acme-1')::uuid, md5('msg-acme-1-2')::uuid, 'link', '2026-11-21 09:00:00+00'),
    (md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, md5('conv-acme-1')::uuid, md5('msg-acme-1-2')::uuid, 'ai_source', '2026-11-21 09:00:00+00');
