-- A reply written after the numbering began, in each workspace: the trigger gives it the next number.
INSERT INTO messages (id, workspace_id, conversation_id, author_agent_id, kind, body, created_at) VALUES
    (md5('msg-acme-numbered')::uuid, md5('ws-acme')::uuid, md5('conv-acme-1')::uuid, md5('agent-acme-ben')::uuid, 'outgoing', 'Numbered reply', '2026-10-22 10:05:00+00'),
    (md5('msg-beta-numbered')::uuid, md5('ws-beta')::uuid, md5('conv-beta-1')::uuid, md5('agent-beta-admin')::uuid, 'outgoing', 'Numbered reply', '2026-10-22 10:06:00+00');
