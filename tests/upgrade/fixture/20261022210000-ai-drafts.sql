-- acme: Alex asked for a draft and sent it after editing (the numbered reply came from it); a second
-- draft was discarded long ago, so its text is gone; a third is still being written. beta: a draft that
-- was replaced by a newer one.
INSERT INTO ai_drafts (id, workspace_id, conversation_id, task_id, requested_by_agent_id, requested_by_token_id, prompt, status, text, missing_info,
        sources, warnings, model, outcome, similarity, sent_message_id, created_at, ready_at, decided_at, decided_by_agent_id, text_cleared_at) VALUES
    (md5('aid-acme-1')::uuid, md5('ws-acme')::uuid, md5('conv-acme-1')::uuid, md5('ait-acme-1')::uuid, md5('agent-acme-ben')::uuid, md5('session-acme-ben')::uuid,
     'Apologise for the delay', 'ready', 'Sorry for the wait. Your order ships today.', 'The tracking number',
     '[{"kind": "kb_article", "id": "0b8d1d0f-7c2a-4d7e-9a7a-2f0d5f1b9c11", "number": 4, "title": "Shipping times"}]',
     '[{"kind": "email", "value": "someone@example.com"}]', 'claude-sonnet-5-5', 'sent_edited', 0.640, md5('msg-acme-numbered')::uuid,
     '2026-10-22 10:00:00+00', '2026-10-22 10:00:05+00', '2026-10-22 10:05:00+00', md5('agent-acme-ben')::uuid, NULL),
    (md5('aid-acme-2')::uuid, md5('ws-acme')::uuid, md5('conv-acme-1')::uuid, NULL, md5('agent-acme-alex')::uuid, NULL,
     '', 'ready', NULL, NULL, '[]', '[]', 'claude-sonnet-5-5', 'discarded', NULL, NULL,
     '2026-07-01 10:00:00+00', '2026-07-01 10:00:05+00', '2026-07-01 10:01:00+00', md5('agent-acme-alex')::uuid, '2026-10-01 00:00:00+00'),
    (md5('aid-acme-3')::uuid, md5('ws-acme')::uuid, md5('conv-acme-1')::uuid, md5('ait-acme-2')::uuid, md5('agent-acme-alex')::uuid, NULL,
     'Short and friendly', 'pending', NULL, NULL, '[]', '[]', NULL, NULL, NULL, NULL,
     '2026-10-22 13:05:00+00', NULL, NULL, NULL, NULL),
    (md5('aid-beta-1')::uuid, md5('ws-beta')::uuid, md5('conv-beta-1')::uuid, NULL, md5('agent-beta-admin')::uuid, NULL,
     '', 'ready', 'Thanks for writing in.', NULL, '[]', '[]', 'llama-4-scout', 'superseded', NULL, NULL,
     '2026-10-22 12:10:00+00', '2026-10-22 12:10:04+00', '2026-10-22 12:11:00+00', md5('agent-beta-admin')::uuid, NULL);

UPDATE messages SET ai_draft_id = md5('aid-acme-1')::uuid WHERE id = md5('msg-acme-numbered')::uuid;
