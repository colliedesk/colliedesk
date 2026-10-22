-- AI retrieval: acme's published article in its own language and in German, and a resolved
-- conversation of beta, already built; beta keeps a published article away from AI, with the version
-- that says who did it; and a source of each kind still waiting to be built.
INSERT INTO kb_articles (id, workspace_id, collection_id, number, title, slug, body_md, status, published_at, author_agent_id, created_at, updated_at,
        ai_excluded, ai_excluded_at, ai_excluded_by_agent_id) VALUES
    (md5('kba-beta-3')::uuid, md5('ws-beta')::uuid, md5('kbc-beta-1')::uuid, 3, 'Internal refund rules', 'internal-refund-rules',
     'Refunds above 100 need a second approval.', 'published', '2026-10-22 11:00:00+00', md5('agent-beta-admin')::uuid,
     '2026-10-22 11:00:00+00', '2026-10-22 11:00:00+00', true, '2026-10-22 11:05:00+00', md5('agent-beta-admin')::uuid);
INSERT INTO kb_article_versions (id, workspace_id, article_id, version, kind, title, slug, body_md, status, collection_id, changed_by_agent_id, created_at) VALUES
    (md5('kba-beta-3-v1')::uuid, md5('ws-beta')::uuid, md5('kba-beta-3')::uuid, 1, 'create', 'Internal refund rules', 'internal-refund-rules',
     'Refunds above 100 need a second approval.', 'published', md5('kbc-beta-1')::uuid, md5('agent-beta-admin')::uuid, '2026-10-22 11:00:00+00'),
    (md5('kba-beta-3-v2')::uuid, md5('ws-beta')::uuid, md5('kba-beta-3')::uuid, 2, 'ai_exclude', 'Internal refund rules', 'internal-refund-rules',
     'Refunds above 100 need a second approval.', 'published', md5('kbc-beta-1')::uuid, md5('agent-beta-admin')::uuid, '2026-10-22 11:05:00+00');

INSERT INTO ai_retrieval_docs (id, workspace_id, kb_article_id, kb_locale, conversation_id, title, body, folded, tokens, updated_at) VALUES
    (md5('aird-acme-1')::uuid, md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, NULL, NULL, 'Reset your password', 'Open **Settings**, then *Password*.',
     kb_search_vector('reset your password open settings then password'), 9, '2026-10-22 12:00:00+00'),
    (md5('aird-acme-2')::uuid, md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, 'de', NULL, 'Passwort zurücksetzen', 'Öffnen Sie **Einstellungen**.',
     kb_search_vector('passwort zurucksetzen offnen sie einstellungen'), 7, '2026-10-22 12:00:00+00'),
    (md5('aird-beta-1')::uuid, md5('ws-beta')::uuid, NULL, NULL, md5('conv-beta-1')::uuid, 'Question 1', E'Question 1\n\nCustomer: Hello [customer]\n\nReply: Numbered reply',
     kb_search_vector('question 1 customer hello customer reply numbered reply'), 12, '2026-10-22 12:00:00+00');

INSERT INTO ai_retrieval_pending (workspace_id, kb_article_id, conversation_id, created_at) VALUES
    (md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, NULL, '2026-10-22 12:01:00+00'),
    (md5('ws-beta')::uuid, NULL, md5('conv-beta-1')::uuid, '2026-10-22 12:01:00+00');
