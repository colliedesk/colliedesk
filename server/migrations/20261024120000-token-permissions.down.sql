-- Code older than this migration knows no custom token. Revoke every custom token and record it as a
-- read token with the agent ceiling, so the older constraints hold and nothing it could do survives;
-- requests a custom token filed keep their record under the read tier. The block marks itself
-- privileged because an owner that is not a superuser sees no row under forced row-level security.
DO $$
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    UPDATE sessions SET revoked_at = COALESCE(revoked_at, now()), scope = 'read', role_ceiling = 'agent', permissions = NULL WHERE scope = 'custom';
    UPDATE agent_requests SET token_scope = 'read' WHERE token_scope = 'custom';
END
$$;
ALTER TABLE agent_requests DROP CONSTRAINT IF EXISTS agent_requests_token_scope_check;
ALTER TABLE agent_requests ADD CONSTRAINT agent_requests_token_scope_check CHECK (token_scope IN ('read', 'draft', 'send', 'admin'));
ALTER TABLE agent_requests DROP COLUMN token_permissions;
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_scope_role_ceiling;
ALTER TABLE sessions ADD CONSTRAINT sessions_scope_role_ceiling
    CHECK (scope IS NULL OR role_ceiling = CASE WHEN scope = 'admin' THEN 'admin' ELSE 'agent' END);
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_custom_permissions;
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_scope_check;
ALTER TABLE sessions ADD CONSTRAINT sessions_scope_check CHECK (scope IN ('read', 'draft', 'send', 'admin'));
ALTER TABLE sessions DROP COLUMN permissions;
