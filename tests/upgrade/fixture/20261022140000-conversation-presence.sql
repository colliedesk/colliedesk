-- Who has a conversation open: a person writing a reply in one tab and viewing in another, an agent
-- token, and a viewer in the second workspace.
INSERT INTO conversation_presence (workspace_id, session_id, client_id, agent_id, conversation_id, writing_since, edited_at, seen_at, expires_at) VALUES
    (md5('ws-acme')::uuid, md5('session-acme-ben')::uuid, 'ben-tab-writing', md5('agent-acme-ben')::uuid, md5('conv-acme-1')::uuid, now() - interval '40 seconds', now() - interval '10 seconds', now() - interval '2 seconds', now() + interval '148 seconds'),
    (md5('ws-acme')::uuid, md5('session-acme-ben')::uuid, 'ben-tab-viewing', md5('agent-acme-ben')::uuid, md5('conv-acme-2')::uuid, NULL, NULL, now() - interval '3 seconds', now() + interval '12 seconds'),
    (md5('ws-acme')::uuid, md5('token-acme-read')::uuid, 'mcp-agent', md5('agent-acme-ben')::uuid, md5('conv-acme-1')::uuid, NULL, NULL, now() - interval '30 seconds', now() + interval '90 seconds'),
    (md5('ws-beta')::uuid, md5('session-beta-admin')::uuid, 'beta-admin-tab', md5('agent-beta-admin')::uuid, md5('conv-beta-1')::uuid, NULL, NULL, now() - interval '1 second', now() + interval '14 seconds');
