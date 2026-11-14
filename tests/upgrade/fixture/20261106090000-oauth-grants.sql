-- OAuth grants: Ben connected Claude Code with OAuth from his browser. Its code was traded, its first
-- refresh token replaced by a second one, and an access token from that second refresh is still valid.
INSERT INTO sessions (id, workspace_id, agent_id, token_hash, role_ceiling, label, created_at, last_seen_at, expires_at, revoked_at, scope,
    oauth_client_id, oauth_redirect_uri, oauth_resource, oauth_connected_at, user_agent) VALUES
    (md5('grant-acme-ben')::uuid, md5('ws-acme')::uuid, md5('agent-acme-ben')::uuid, sha256('fixture-grant-ben'), 'agent', 'Claude Code',
     now() - interval '3 days', now() - interval '10 minutes', now() + interval '30 days', NULL, 'oauth',
     'https://claude.ai/oauth/claude-code-client-metadata', 'http://localhost/callback', 'https://desk.example.com/mcp', now() - interval '3 days',
     'Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/605.1.15 Safari/605.1.15');
INSERT INTO oauth_tokens (workspace_id, session_id, kind, hash, code_challenge, created_at, expires_at, replaced_at) VALUES
    (md5('ws-acme')::uuid, md5('grant-acme-ben')::uuid, 'code', sha256('fixture-code-ben'), 'E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM',
     now() - interval '3 days', now() - interval '3 days' + interval '5 minutes', now() - interval '3 days'),
    (md5('ws-acme')::uuid, md5('grant-acme-ben')::uuid, 'refresh', sha256('fixture-refresh-ben-1'), NULL,
     now() - interval '3 days', now() + interval '27 days', now() - interval '10 minutes'),
    (md5('ws-acme')::uuid, md5('grant-acme-ben')::uuid, 'refresh', sha256('fixture-refresh-ben-2'), NULL,
     now() - interval '10 minutes', now() + interval '30 days', NULL),
    (md5('ws-acme')::uuid, md5('grant-acme-ben')::uuid, 'access', sha256('fixture-access-ben-2'), NULL,
     now() - interval '10 minutes', now() + interval '50 minutes', NULL);
