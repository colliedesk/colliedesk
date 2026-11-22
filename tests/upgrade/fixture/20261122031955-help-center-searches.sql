-- acme readers searched twice, opened the password article from one search and found nothing for
-- the other; a beta reader's search listed one article and opened it.
INSERT INTO kb_searches (workspace_id, query, query_key, results, opened_article_id, created_at) VALUES
    (md5('ws-acme')::uuid, 'Reset password', 'reset password', 2, md5('kba-acme-1')::uuid, '2026-11-21 10:00:00+00'),
    (md5('ws-acme')::uuid, 'invoice [number removed]', 'invoice [number removed]', 0, NULL, '2026-11-21 10:05:00+00'),
    (md5('ws-beta')::uuid, 'login', 'login', 1, md5('kba-beta-1')::uuid, '2026-11-21 11:00:00+00');
