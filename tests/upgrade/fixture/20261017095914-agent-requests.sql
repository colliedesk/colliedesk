-- Requests an AI agent made that a person approved, rejected or has not decided yet.
INSERT INTO agent_requests (id, workspace_id, token_id, token_label, token_scope, requested_by_agent_id, kind, method, route, path, body, edited_body, summary, conversation_id, status, decided_by_agent_id, decided_at, reason, result_status, result, result_note, created_at, expires_at) VALUES
    (md5('request-beta-approved')::uuid, md5('ws-beta')::uuid, md5('token-beta-draft')::uuid, 'Claude (draft)', 'draft', md5('agent-beta-cy')::uuid, 'reply_draft', 'POST',
     '/api/v1/conversations/{id}/messages', '/api/v1/conversations/' || md5('conv-beta-8')::uuid || '/messages',
     '{"kind": "outgoing", "body": "Here is your refund."}', '{"kind": "outgoing", "body": "Here is your refund, sorry for the wait."}',
     'Reply to Question 8', md5('conv-beta-8')::uuid, 'approved', md5('agent-beta-admin')::uuid, '2026-10-17 09:00:00+00', NULL, 201, '{"id": "fixture"}', 'Sent with the edit',
     '2026-10-17 08:40:00+00', '2026-10-18 08:40:00+00'),
    (md5('request-beta-rejected')::uuid, md5('ws-beta')::uuid, md5('token-beta-draft')::uuid, 'Claude (draft)', 'draft', md5('agent-beta-cy')::uuid, 'action', 'PATCH',
     '/api/v1/conversations/{id}', '/api/v1/conversations/' || md5('conv-beta-9')::uuid, '{"status": "closed"}', NULL,
     'Close Question 9', md5('conv-beta-9')::uuid, 'rejected', md5('agent-beta-admin')::uuid, '2026-10-17 09:05:00+00', 'Not solved yet', NULL, NULL, NULL,
     '2026-10-17 08:45:00+00', '2026-10-18 08:45:00+00'),
    (md5('request-beta-pending')::uuid, md5('ws-beta')::uuid, md5('token-beta-draft')::uuid, 'Claude (draft)', 'draft', md5('agent-beta-cy')::uuid, 'reply_draft', 'POST',
     '/api/v1/conversations/{id}/messages', '/api/v1/conversations/' || md5('conv-beta-10')::uuid || '/messages', '{"kind": "outgoing", "body": "Draft answer"}', NULL,
     'Reply to Question 10', md5('conv-beta-10')::uuid, 'pending', NULL, NULL, NULL, NULL, NULL, NULL,
     now() - interval '1 hour', now() + interval '23 hours');
-- The reply the approved request sent.
INSERT INTO messages (id, workspace_id, conversation_id, author_agent_id, kind, body, created_at, agent_request_id)
VALUES (md5('msg-beta-8-approved')::uuid, md5('ws-beta')::uuid, md5('conv-beta-8')::uuid, md5('agent-beta-admin')::uuid, 'outgoing',
        'Here is your refund, sorry for the wait.', '2026-10-17 09:00:01+00', md5('request-beta-approved')::uuid);
