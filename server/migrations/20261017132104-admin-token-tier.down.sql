-- Code older than this migration knows no admin tier. Revoke every admin token and record it as a
-- send token with the agent ceiling, so the older constraints hold and nothing it could do survives.
-- Requests an admin token filed keep their record under the send tier. The block marks itself
-- privileged because an owner that is not a superuser sees no row under forced row-level security.
DO $$
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    UPDATE sessions SET revoked_at = COALESCE(revoked_at, now()), scope = 'send', role_ceiling = 'agent' WHERE scope = 'admin';
    UPDATE agent_requests SET token_scope = 'send' WHERE token_scope = 'admin';
END
$$;
ALTER TABLE agent_requests DROP CONSTRAINT IF EXISTS agent_requests_token_scope_check;
ALTER TABLE agent_requests ADD CONSTRAINT agent_requests_token_scope_check CHECK (token_scope IN ('read', 'draft', 'send'));
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_scope_role_ceiling;
ALTER TABLE sessions ADD CONSTRAINT sessions_scope_agent_ceiling CHECK (scope IS NULL OR role_ceiling = 'agent');
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_scope_check;
ALTER TABLE sessions ADD CONSTRAINT sessions_scope_check CHECK (scope IN ('read', 'draft', 'send'));
