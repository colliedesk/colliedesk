-- Code older than this migration ignores the tier, so a token left active would become an ordinary
-- agent session. Revoke every token first. The block marks itself privileged because an owner that
-- is not a superuser sees no session row under forced row-level security otherwise.
DO $$
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    UPDATE sessions SET revoked_at = now() WHERE scope IS NOT NULL AND revoked_at IS NULL;
END
$$;
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_scope_agent_ceiling;
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_scope_check;
ALTER TABLE sessions DROP COLUMN IF EXISTS scope;
