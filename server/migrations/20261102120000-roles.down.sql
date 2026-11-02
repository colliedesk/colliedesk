-- Back to the fixed roles and token tiers. A token that follows a role has no tier to go back to, so it
-- goes; the old columns were kept up to date, so everything else signs in as before.
SELECT set_config('app.privileged', 'on', true);
DELETE FROM sessions WHERE scope = 'role';
-- The requests the upgrade closed wait again, and the drafts it wrote from them go: the previous release
-- shows those replies as requests. A reply a token wrote as a draft after the upgrade has no place in the
-- previous release (it would pass for an AI draft) and is dropped with them.
UPDATE agent_requests SET status = 'pending', decided_at = NULL, reason = NULL
WHERE status = 'expired'
    AND reason = 'Colliedesk removed agent requests: nothing ran. An agent token now acts at once when it holds the permission.';
DELETE FROM ai_drafts WHERE source = 'written';
ALTER TABLE ai_drafts DROP COLUMN cc;
ALTER TABLE ai_drafts DROP COLUMN source;
ALTER TABLE sessions DROP CONSTRAINT sessions_scope_role_ceiling;
ALTER TABLE sessions ADD CONSTRAINT sessions_scope_role_ceiling
    CHECK (scope IS NULL OR scope = 'custom' OR role_ceiling = CASE WHEN scope = 'admin' THEN 'admin' ELSE 'agent' END);
ALTER TABLE sessions DROP CONSTRAINT sessions_role_token;
ALTER TABLE sessions DROP CONSTRAINT sessions_scope_check;
ALTER TABLE sessions ADD CONSTRAINT sessions_scope_check CHECK (scope IN ('read', 'draft', 'send', 'admin', 'custom'));
DROP INDEX sessions_role_idx;
ALTER TABLE sessions DROP CONSTRAINT sessions_role_fkey;
ALTER TABLE sessions DROP COLUMN legacy;
ALTER TABLE sessions DROP COLUMN role_id;
DROP TABLE invite_roles;
ALTER TABLE pairing_links DROP CONSTRAINT pairing_links_workspace_id_id_key;
DROP TRIGGER agents_starting_role ON agents;
DROP FUNCTION agents_starting_role();
DROP TABLE agent_roles;
DROP TRIGGER workspaces_system_roles ON workspaces;
DROP FUNCTION workspaces_system_roles();
DROP TABLE roles;
DROP FUNCTION colliedesk_agent_role_permissions();
