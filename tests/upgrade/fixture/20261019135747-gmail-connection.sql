-- beta sends through Gmail; cobalt's Gmail grant was revoked. The server seals a refresh token
-- (tokenseal) with a key kept outside the database, so these bytes stand in for sealed tokens: an
-- upgrade must keep them exactly as they are.
INSERT INTO mailbox_connections (workspace_id, provider, address, scopes, token_key_id, refresh_token_sealed, status, error_code, failed_at, connected_by_agent_id, connected_at, updated_at) VALUES
    (md5('ws-beta')::uuid, 'google', 'support@beta.example', 'https://www.googleapis.com/auth/gmail.send', 'fixture-key-1',
     decode('01' || md5('nonce') || md5('sealed-refresh-token') || md5('tag'), 'hex'), 'connected', NULL, NULL, md5('agent-beta-admin')::uuid,
     '2026-10-19 14:00:00+00', '2026-10-19 14:00:00+00'),
    (md5('ws-cobalt')::uuid, 'google', 'help@cobalt.example', 'https://www.googleapis.com/auth/gmail.send', 'fixture-key-1',
     decode('01' || md5('nonce-cobalt') || md5('sealed-refresh-token-cobalt') || md5('tag-cobalt'), 'hex'), 'failed', 'revoked', '2026-10-19 15:00:00+00',
     md5('agent-cobalt-admin')::uuid, '2026-10-19 14:30:00+00', '2026-10-19 15:00:00+00');
INSERT INTO mailbox_oauth_states (state_hash, workspace_id, agent_id, provider, code_verifier, expires_at, created_at)
VALUES (sha256('fixture-oauth-state'), md5('ws-acme')::uuid, md5('agent-acme-admin')::uuid, 'google', 'fixture-code-verifier', now() + interval '5 minutes', now());
UPDATE email_outbox SET transport = 'gmail' WHERE workspace_id = md5('ws-beta')::uuid AND id = md5('outbox-beta-23')::uuid;
