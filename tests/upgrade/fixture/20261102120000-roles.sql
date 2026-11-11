-- acme made a role for its knowledge base and gave it to Ben, who follows it with an agent token; it
-- deleted an older one. beta's open invite gives the Agent role and the KB role is not beta's. A token
-- left a written reply draft with a copy.
INSERT INTO roles (id, workspace_id, name, description, permissions, created_by_agent_id, updated_by_agent_id, created_at, updated_at) VALUES
    (md5('role-acme-kb')::uuid, md5('ws-acme')::uuid, 'Knowledge base editor', 'Writes and publishes articles',
     ARRAY['kb_articles:read', 'kb_articles:create', 'kb_articles:update', 'kb_articles:publish', 'kb_media:read'],
     md5('agent-acme-admin')::uuid, md5('agent-acme-ben')::uuid, '2026-11-02 09:00:00+00', '2026-11-02 09:30:00+00');
INSERT INTO roles (id, workspace_id, name, permissions, created_by_agent_id, created_at, updated_at, deleted_at, deleted_by_agent_id) VALUES
    (md5('role-acme-old')::uuid, md5('ws-acme')::uuid, 'Night shift', ARRAY['conversations:read'],
     md5('agent-acme-admin')::uuid, '2026-11-02 08:00:00+00', '2026-11-02 08:00:00+00', '2026-11-02 08:30:00+00', md5('agent-acme-admin')::uuid);
INSERT INTO agent_roles (workspace_id, agent_id, role_id, granted_by_agent_id, granted_at) VALUES
    (md5('ws-acme')::uuid, md5('agent-acme-ben')::uuid, md5('role-acme-kb')::uuid, md5('agent-acme-admin')::uuid, '2026-11-02 09:05:00+00');
INSERT INTO sessions (id, workspace_id, agent_id, token_hash, role_ceiling, label, created_at, last_seen_at, expires_at, revoked_at, scope, role_id) VALUES
    (md5('token-acme-role')::uuid, md5('ws-acme')::uuid, md5('agent-acme-ben')::uuid, sha256('fixture-token-role'), 'agent', 'KB agent (role)',
     now() - interval '1 day', now() - interval '1 hour', now() + interval '80 days', NULL, 'role', md5('role-acme-kb')::uuid);
INSERT INTO ai_drafts (workspace_id, conversation_id, requested_by_agent_id, requested_by_token_id, status, source, text, cc, ready_at, created_at) VALUES
    (md5('ws-acme')::uuid, md5('conv-acme-2')::uuid, md5('agent-acme-ben')::uuid, md5('token-acme-role')::uuid, 'ready', 'written',
     'Thanks, the refund is on its way.', ARRAY['billing@customer.example'], now() - interval '1 hour', now() - interval '1 hour');
