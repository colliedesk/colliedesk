-- A note an AI agent wrote with its token.
INSERT INTO messages (id, workspace_id, conversation_id, author_agent_id, kind, body, created_at, agent_token_id)
VALUES (md5('msg-beta-7-token-note')::uuid, md5('ws-beta')::uuid, md5('conv-beta-7')::uuid, md5('agent-beta-cy')::uuid, 'note',
        'Drafted by Claude: customer wants a refund.', '2026-10-17 08:30:00+00', md5('token-beta-draft')::uuid);
