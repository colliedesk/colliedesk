-- A sent reply in each workspace disagreed with a published article. acme's flag is still open;
-- beta's admin cleared theirs. acme checked one reply today, and the beta reply below waits in the queue.
INSERT INTO messages (id, workspace_id, conversation_id, author_agent_id, kind, body, created_at) VALUES
    (md5('msg-acme-conflict')::uuid, md5('ws-acme')::uuid, md5('conv-acme-1')::uuid, md5('agent-acme-ben')::uuid,
        'outgoing', 'You can reset the password from the sign-in page within 48 hours.', '2026-11-23 09:00:00+00'),
    (md5('msg-beta-conflict')::uuid, md5('ws-beta')::uuid, md5('conv-beta-1')::uuid, md5('agent-beta-admin')::uuid,
        'outgoing', 'Exports are ready within 2 hours.', '2026-11-23 09:10:00+00');
UPDATE kb_conflict_checks SET checked_at = '2026-11-23 09:00:30+00'
    WHERE workspace_id = md5('ws-acme')::uuid AND message_id = md5('msg-acme-conflict')::uuid;
INSERT INTO kb_article_conflicts (id, workspace_id, article_id, message_id, reason, created_at,
        cleared_at, cleared_by_agent_id) VALUES
    (md5('kbc-acme-1')::uuid, md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, md5('msg-acme-conflict')::uuid,
        'The reply says "within 48 hours", the article says "within 24 hours".', '2026-11-23 09:00:40+00',
        NULL, NULL),
    (md5('kbc-beta-1')::uuid, md5('ws-beta')::uuid, md5('kba-beta-1')::uuid, md5('msg-beta-conflict')::uuid,
        'The reply says "within 2 hours", the article says "within a day".', '2026-11-23 09:11:00+00',
        '2026-11-23 10:00:00+00', md5('agent-beta-admin')::uuid);
