-- acme: the new ticket's automatic suggestion was partly accepted (the tag and the priority, not the
-- assignee); Ben asked again by hand later and dismissed that one. beta: the automatic task was
-- cancelled when triage was turned off, so its suggestion reads as cancelled.
INSERT INTO ai_triage_suggestions (id, workspace_id, conversation_id, task_id, automatic, requested_by_agent_id, requested_by_token_id, status, tag_ids,
        priority, assignee_agent_id, reason, model, outcome, applied, created_at, ready_at, decided_at, decided_by_agent_id) VALUES
    (md5('ats-acme-1')::uuid, md5('ws-acme')::uuid, md5('conv-acme-1')::uuid, md5('ait-acme-1')::uuid, true, NULL, NULL, 'ready',
     ARRAY[md5('tag-acme-bug')::uuid, md5('tag-acme-vip')::uuid], 'high', md5('agent-acme-ben')::uuid, 'A broken checkout for a large customer.', 'claude-haiku-5-5',
     'partly_accepted', jsonb_build_object('tag_ids', jsonb_build_array(md5('tag-acme-bug')::uuid::text), 'priority', 'high', 'assignee_agent_id', NULL),
     '2026-10-22 09:00:00+00', '2026-10-22 09:00:03+00', '2026-10-22 09:10:00+00', md5('agent-acme-ben')::uuid),
    (md5('ats-acme-2')::uuid, md5('ws-acme')::uuid, md5('conv-acme-1')::uuid, NULL, false, md5('agent-acme-ben')::uuid, md5('session-acme-ben')::uuid, 'ready',
     '{}', 'normal', NULL, 'Nothing urgent.', 'claude-haiku-5-5', 'dismissed', '{}',
     '2026-10-22 11:00:00+00', '2026-10-22 11:00:02+00', '2026-10-22 11:05:00+00', md5('agent-acme-ben')::uuid),
    (md5('ats-beta-1')::uuid, md5('ws-beta')::uuid, md5('conv-beta-1')::uuid, md5('ait-beta-1')::uuid, true, NULL, NULL, 'pending',
     '{}', NULL, NULL, NULL, NULL, NULL, '{}', '2026-10-22 12:00:00+00', NULL, NULL, NULL);
