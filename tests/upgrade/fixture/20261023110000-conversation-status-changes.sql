-- Status history: a person resolves and a customer reply reopens in the first workspace, an import
-- re-run closes one in the second.
INSERT INTO conversation_status_changes (workspace_id, id, conversation_id, from_status, to_status, changed_at, actor_agent_id, source) VALUES
    (md5('ws-acme')::uuid, md5('status-change-acme-1')::uuid, md5('conv-acme-1')::uuid, 'open', 'resolved', now() - interval '3 days', md5('agent-acme-ben')::uuid, 'colliedesk'),
    (md5('ws-acme')::uuid, md5('status-change-acme-2')::uuid, md5('conv-acme-1')::uuid, 'resolved', 'open', now() - interval '2 days', NULL, 'colliedesk'),
    (md5('ws-beta')::uuid, md5('status-change-beta-1')::uuid, md5('conv-beta-1')::uuid, 'open', 'closed', now() - interval '1 day', md5('agent-beta-admin')::uuid, 'import');
