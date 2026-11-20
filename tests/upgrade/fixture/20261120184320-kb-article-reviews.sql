-- acme requires approval in its Getting started collection. Ben sent the article for review through his
-- KB writer token, edited it while the admin was reviewing (the first review was superseded, the second
-- is open). beta requires approval everywhere: the admin sent the Portuguese translation for review
-- through Codex, and Cy approved it through his own token; it waits to be published.
UPDATE help_centers SET require_approval = true WHERE workspace_id = md5('ws-beta')::uuid;
UPDATE kb_collections SET require_approval = 'on' WHERE id = md5('kbc-acme-1')::uuid;

INSERT INTO kb_reviews (id, workspace_id, article_id, locale, content_hash, state, note, requested_by_agent_id, requested_by_token_id, requested_by_token_label,
        decided_by_agent_id, decided_by_token_id, decided_by_token_label, decided_at, decision_note, supersedes_review_id, approves_pending, author_email,
        author_email_claimed_at, created_at) VALUES
    (md5('kbr-acme-1')::uuid, md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, NULL, repeat('a', 64), 'superseded', 'Check the steps', md5('agent-acme-ben')::uuid,
     md5('token-acme-kb')::uuid, 'KB writer (custom)', NULL, NULL, NULL, '2026-11-20 18:50:00+00', '', NULL, false, 'none', NULL, '2026-11-20 18:45:00+00'),
    (md5('kbr-acme-2')::uuid, md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, NULL, repeat('b', 64), 'open', 'Check the steps', md5('agent-acme-ben')::uuid,
     md5('token-acme-kb')::uuid, 'KB writer (custom)', NULL, NULL, NULL, NULL, '', md5('kbr-acme-1')::uuid, false, 'none', NULL, '2026-11-20 18:50:00+00'),
    (md5('kbr-beta-1')::uuid, md5('ws-beta')::uuid, md5('kba-beta-1')::uuid, 'pt-BR', repeat('c', 64), 'approved', '', md5('agent-beta-admin')::uuid,
     md5('token-beta-send')::uuid, 'Codex (send)', md5('agent-beta-cy')::uuid, md5('token-beta-draft')::uuid, 'Claude (draft)', '2026-11-20 19:10:00+00',
     'Reads well', NULL, true, 'sent', '2026-11-20 19:10:30+00', '2026-11-20 19:00:00+00');

INSERT INTO kb_review_reviewers (workspace_id, review_id, agent_id, email, email_claimed_at, notified_at, seen_at, created_at) VALUES
    (md5('ws-acme')::uuid, md5('kbr-acme-1')::uuid, md5('agent-acme-admin')::uuid, 'sent', '2026-11-20 18:45:10+00', '2026-11-20 18:45:12+00', '2026-11-20 18:48:00+00', '2026-11-20 18:45:00+00'),
    (md5('ws-acme')::uuid, md5('kbr-acme-2')::uuid, md5('agent-acme-admin')::uuid, 'queued', NULL, NULL, NULL, '2026-11-20 18:50:00+00'),
    (md5('ws-beta')::uuid, md5('kbr-beta-1')::uuid, md5('agent-beta-cy')::uuid, 'sent', '2026-11-20 19:00:10+00', '2026-11-20 19:00:12+00', '2026-11-20 19:05:00+00', '2026-11-20 19:00:00+00');
