-- Token permissions: a custom token that may only write knowledge base drafts, and a request it filed
-- to publish an article, which keeps what the token held.
INSERT INTO sessions (id, workspace_id, agent_id, token_hash, role_ceiling, label, created_at, last_seen_at, expires_at, revoked_at, scope, permissions) VALUES
    (md5('token-acme-kb')::uuid, md5('ws-acme')::uuid, md5('agent-acme-ben')::uuid, sha256('fixture-token-kb'), 'agent', 'KB writer (custom)',
     now() - interval '2 days', now() - interval '1 hour', now() + interval '80 days', NULL, 'custom',
     ARRAY['kb_articles:create', 'kb_articles:read', 'kb_articles:update', 'kb_media:read']);
INSERT INTO agent_requests (id, workspace_id, token_id, token_label, token_scope, token_permissions, requested_by_agent_id, kind, method, route, path, body, summary, status, created_at, expires_at) VALUES
    (md5('request-acme-kb-publish')::uuid, md5('ws-acme')::uuid, md5('token-acme-kb')::uuid, 'KB writer (custom)', 'custom',
     ARRAY['kb_articles:create', 'kb_articles:read', 'kb_articles:update', 'kb_media:read'], md5('agent-acme-ben')::uuid, 'action', 'POST',
     '/api/v1/kb/articles', '/api/v1/kb/articles', '{"collection_id": "00000000-0000-4000-8000-000000000000", "title": "Refunds", "body_md": "How refunds work.", "status": "published"}',
     'Create article Refunds (published)', 'pending', now() - interval '1 hour', now() + interval '6 days');
