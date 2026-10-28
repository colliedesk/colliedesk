-- cobalt replaced its revoked Gmail grant with a Microsoft 365 mailbox, which then lost its Exchange
-- Online licence; a reply waited for it and failed, and a second consent for it is in flight. The sealed
-- bytes stand in for a sealed refresh token, as in the Gmail fixture.
UPDATE mailbox_connections SET provider = 'microsoft', account_id = '6f1c2a90-0b8e-4d55-9a63-2f7e1c0d4b11',
    scopes = 'openid offline_access https://graph.microsoft.com/User.Read https://graph.microsoft.com/Mail.Send',
    refresh_token_sealed = decode('01' || md5('nonce-cobalt-ms') || md5('sealed-refresh-token-cobalt-ms') || md5('tag-cobalt-ms'), 'hex'),
    error_code = 'mailbox_unavailable', failed_at = '2026-10-25 12:30:00+00', connected_at = '2026-10-25 12:00:00+00', updated_at = '2026-10-25 12:30:00+00'
    WHERE workspace_id = md5('ws-cobalt')::uuid;
INSERT INTO mailbox_oauth_states (state_hash, workspace_id, agent_id, provider, code_verifier, expires_at, created_at, channel_id)
    SELECT sha256('fixture-oauth-state-cobalt-ms'), workspace_id, md5('agent-cobalt-admin')::uuid, 'microsoft', 'fixture-code-verifier-cobalt-ms',
        now() + interval '5 minutes', now(), channel_id
    FROM mailbox_connections WHERE workspace_id = md5('ws-cobalt')::uuid;
UPDATE channel_outbox SET transport = 'microsoft', status = 'failed', locked_until = NULL, claim_token = NULL,
    last_error = 'This Microsoft account has no Exchange Online mailbox any more. Reconnect Microsoft 365 in Settings, then retry the reply.'
    WHERE id = (SELECT id FROM channel_outbox WHERE workspace_id = md5('ws-cobalt')::uuid AND channel_type = 'email' ORDER BY created_at, id LIMIT 1);
