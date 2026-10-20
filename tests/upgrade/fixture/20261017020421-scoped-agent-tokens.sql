-- Agent tokens (sessions with a tier) that AI agents use through MCP.
INSERT INTO sessions (id, workspace_id, agent_id, token_hash, role_ceiling, label, created_at, last_seen_at, expires_at, revoked_at, scope) VALUES
    (md5('token-beta-draft')::uuid, md5('ws-beta')::uuid, md5('agent-beta-cy')::uuid, sha256('fixture-token-1'), 'agent', 'Claude (draft)', '2026-10-17 08:00:00+00', now() - interval '1 hour', now() + interval '80 days', NULL, 'draft'),
    (md5('token-beta-send')::uuid, md5('ws-beta')::uuid, md5('agent-beta-admin')::uuid, sha256('fixture-token-2'), 'agent', 'Codex (send)', '2026-10-17 08:05:00+00', '2026-10-17 09:00:00+00', now() + interval '80 days', '2026-10-17 10:00:00+00', 'send'),
    (md5('token-acme-read')::uuid, md5('ws-acme')::uuid, md5('agent-acme-ben')::uuid, sha256('fixture-token-3'), 'agent', 'Reporting (read)', '2026-10-17 08:10:00+00', now() - interval '1 day', now() + interval '80 days', NULL, 'read');
