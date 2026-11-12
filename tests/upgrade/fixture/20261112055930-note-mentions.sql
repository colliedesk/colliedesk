-- acme's admin asks Ben on a note; Ben read it and was emailed. Alex was asked too and has not read it.
INSERT INTO messages (id, workspace_id, conversation_id, author_agent_id, kind, body, created_at) VALUES
    (md5('msg-acme-mention')::uuid, md5('ws-acme')::uuid, md5('conv-acme-1')::uuid, md5('agent-acme-admin')::uuid, 'note', '@Ben @Alex can you check the refund?', '2026-11-12 09:24:00+00');
INSERT INTO note_mentions (id, workspace_id, message_id, agent_id, created_at, read_at, email, email_claimed_at) VALUES
    (md5('mention-acme-ben')::uuid, md5('ws-acme')::uuid, md5('msg-acme-mention')::uuid, md5('agent-acme-ben')::uuid, '2026-11-12 09:24:00+00', '2026-11-12 09:40:00+00', 'sent', '2026-11-12 09:24:10+00'),
    (md5('mention-acme-alex')::uuid, md5('ws-acme')::uuid, md5('msg-acme-mention')::uuid, md5('agent-acme-alex')::uuid, '2026-11-12 09:24:01+00', NULL, 'queued', NULL);
UPDATE agents SET mention_email = false WHERE id = md5('agent-acme-gone')::uuid;
