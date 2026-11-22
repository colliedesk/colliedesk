-- acme has two knowledge gaps: "invoice [number removed]", found by a search with no results and
-- marked fixed by the password article it later matched, and "cancel my plan", which an admin
-- dismissed after two readers voted "No". One more search with no results waits in the queue.
INSERT INTO kb_gaps (id, workspace_id, title, query_key, status, fixed_by_article_id,
        fixed_automatically, askers, first_seen, last_seen, status_changed_by_agent_id,
        status_changed_at, created_at) VALUES
    (md5('gap-acme-1')::uuid, md5('ws-acme')::uuid, 'invoice [number removed]', 'invoice [number removed]',
        'fixed', md5('kba-acme-1')::uuid, true, 1, '2026-11-21 10:05:00+00', '2026-11-21 10:05:00+00',
        NULL, '2026-11-22 09:00:00+00', '2026-11-21 10:06:00+00'),
    (md5('gap-acme-2')::uuid, md5('ws-acme')::uuid, 'cancel my plan', 'cancel my plan',
        'dismissed', NULL, false, 2, '2026-11-21 12:00:00+00', '2026-11-21 13:00:00+00',
        md5('agent-acme-admin')::uuid, '2026-11-22 10:00:00+00', '2026-11-21 12:01:00+00');
INSERT INTO kb_gap_signals (id, workspace_id, gap_id, kind, source_id, text, asker_key, created_at) VALUES
    (md5('gap-signal-acme-1')::uuid, md5('ws-acme')::uuid, md5('gap-acme-1')::uuid, 'search_no_result',
        '2', 'invoice [number removed]', sha256('search:2'::bytea), '2026-11-21 10:05:00+00'),
    (md5('gap-signal-acme-2')::uuid, md5('ws-acme')::uuid, md5('gap-acme-2')::uuid, 'vote_no',
        md5('vote-acme-x')::uuid::text, 'cancel my plan', sha256('vote:reader-1'::bytea), '2026-11-21 12:00:00+00'),
    (md5('gap-signal-acme-3')::uuid, md5('ws-acme')::uuid, md5('gap-acme-2')::uuid, 'vote_no',
        md5('vote-acme-y')::uuid::text, 'cancel my plan', sha256('vote:reader-2'::bytea), '2026-11-21 13:00:00+00');
INSERT INTO kb_gap_pending (workspace_id, kind, source_id, created_at) VALUES
    (md5('ws-acme')::uuid, 'search_no_result', '3', '2026-11-22 11:00:00+00');
