-- Roles replace the two fixed roles (admin, agent) and the tiers of agent tokens (#roles). A workspace
-- has roles, each a list of permissions (resource:action, checked in Go by auth.ValidPermission like
-- sessions.permissions); a person holds one or more roles and every permission of each. Two roles are
-- built in: Admin (system 'admin', permissions NULL: every permission, also ones a later release adds)
-- and Agent (system 'agent', the permissions the agent role had). An agent token follows one role of its
-- person (scope 'role') or holds a fixed list (scope 'custom'); either way a call never gets more than
-- its person holds at that moment. Agent requests are gone: a token acts at once when it holds the
-- permission. The old columns (agents.role, pairing_links.role_ceiling, sessions.role_ceiling, the tier
-- in sessions.scope) stay and are still written, so a rollback from a backup works; nothing decides on
-- them any more.

CREATE TABLE roles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 60),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 300),
    system text CHECK (system IN ('admin', 'agent')),
    permissions text[],
    created_by_agent_id uuid,
    updated_by_agent_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- Deleting a role only marks it: revoked tokens and used invites still name it.
    deleted_at timestamptz,
    deleted_by_agent_id uuid,
    UNIQUE (workspace_id, id),
    CHECK ((system IS NOT DISTINCT FROM 'admin') = (permissions IS NULL)),
    CHECK (system IS NULL OR deleted_at IS NULL),
    FOREIGN KEY (workspace_id, created_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (created_by_agent_id),
    FOREIGN KEY (workspace_id, updated_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (updated_by_agent_id),
    FOREIGN KEY (workspace_id, deleted_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (deleted_by_agent_id)
);
CREATE UNIQUE INDEX roles_name_idx ON roles(workspace_id, lower(btrim(name))) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX roles_system_idx ON roles(workspace_id, system) WHERE system IS NOT NULL;

-- Who holds which role. Taking a role from someone deletes the row.
CREATE TABLE agent_roles (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id uuid NOT NULL,
    role_id uuid NOT NULL,
    granted_by_agent_id uuid,
    granted_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, agent_id, role_id),
    FOREIGN KEY (workspace_id, agent_id) REFERENCES agents(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, role_id) REFERENCES roles(workspace_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, granted_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (granted_by_agent_id)
);
CREATE INDEX agent_roles_role_idx ON agent_roles(workspace_id, role_id);

-- The roles an invite gives when it is redeemed, added to any the person holds.
ALTER TABLE pairing_links ADD CONSTRAINT pairing_links_workspace_id_id_key UNIQUE (workspace_id, id);
CREATE TABLE invite_roles (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    link_id uuid NOT NULL,
    role_id uuid NOT NULL,
    PRIMARY KEY (workspace_id, link_id, role_id),
    FOREIGN KEY (workspace_id, link_id) REFERENCES pairing_links(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, role_id) REFERENCES roles(workspace_id, id) ON DELETE RESTRICT
);
CREATE INDEX invite_roles_role_idx ON invite_roles(workspace_id, role_id);

DO $$
DECLARE
    app_role text;
    t text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    FOREACH t IN ARRAY ARRAY['roles', 'agent_roles', 'invite_roles'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format($policy$CREATE POLICY workspace_isolation ON %2$I FOR ALL TO %1$I
            USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
            WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role, t);
        EXECUTE format($policy$CREATE POLICY privileged_access ON %2$I FOR ALL TO %1$I
            USING ((SELECT current_setting('app.privileged', true) = 'on'))
            WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user, t);
    END LOOP;
    EXECUTE format('GRANT SELECT, INSERT, UPDATE ON roles TO %I', app_role);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON agent_roles TO %I', app_role);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON invite_roles TO %I', app_role);
END
$$;

-- The permissions the built-in Agent role starts with: what the agent role could do before roles, as
-- agent tokens named it, plus team:create_tokens (a person can only give a token what they hold).
-- auth.AgentRolePermissions must stay equal to this list (TestTheAgentRoleStartsAsTheMigrationMadeIt).
CREATE FUNCTION colliedesk_agent_role_permissions() RETURNS text[] LANGUAGE sql IMMUTABLE AS $$
    SELECT ARRAY['conversations:read', 'conversations:update', 'messages:read', 'messages:note', 'messages:send',
        'ai_assist:read', 'ai_assist:create', 'contacts:read', 'contacts:create', 'contacts:update', 'tags:read',
        'tags:create', 'tags:update', 'tags:delete', 'saved_replies:read', 'saved_replies:create',
        'saved_replies:update', 'saved_replies:delete', 'saved_views:read', 'saved_views:create',
        'saved_views:update', 'saved_views:delete', 'kb_articles:read', 'kb_articles:create', 'kb_articles:update',
        'kb_articles:delete', 'kb_articles:publish', 'kb_media:read', 'kb_media:create', 'kb_media:update',
        'kb_media:delete', 'help_center:read', 'results:read', 'profile:update', 'team:read', 'channels:read',
        'ai_settings:read', 'autofix:read', 'autofix:update', 'autofix:send', 'autofix_settings:read',
        'team:create_tokens']::text[]
$$;

-- Every new workspace gets its two built-in roles, whichever way it is made (create-workspace, cloud
-- sign-up, tests). A restore runs with triggers off and copies the roles it saved.
CREATE FUNCTION workspaces_system_roles() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO roles(workspace_id, name, system, permissions) VALUES
        (NEW.id, 'Admin', 'admin', NULL),
        (NEW.id, 'Agent', 'agent', colliedesk_agent_role_permissions());
    RETURN NEW;
END
$$;
CREATE TRIGGER workspaces_system_roles AFTER INSERT ON workspaces FOR EACH ROW EXECUTE FUNCTION workspaces_system_roles();

-- The rows below are every workspace's; an owner that is not a superuser sees none under forced
-- row-level security without this.
SELECT set_config('app.privileged', 'on', true);

INSERT INTO roles(workspace_id, name, system, permissions, created_at, updated_at)
SELECT w.id, r.name, r.system, r.permissions, w.created_at, w.created_at
FROM workspaces w CROSS JOIN (VALUES ('Admin', 'admin', NULL::text[]), ('Agent', 'agent', colliedesk_agent_role_permissions())) AS r(name, system, permissions);

-- Everyone keeps the role they had, deactivated people too (reactivating brings it back).
INSERT INTO agent_roles(workspace_id, agent_id, role_id, granted_at)
SELECT a.workspace_id, a.id, r.id, a.created_at
FROM agents a JOIN roles r ON r.workspace_id = a.workspace_id AND r.system = a.role;

-- A new member starts with the built-in role its role column names, so no way of adding someone (the
-- first admin, an invite, an import, a test) leaves them without a role. Code that gives other roles,
-- such as an invite with its own, replaces this one in the same transaction.
CREATE FUNCTION agents_starting_role() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO agent_roles(workspace_id, agent_id, role_id, granted_at)
    SELECT NEW.workspace_id, NEW.id, r.id, NEW.created_at FROM roles r WHERE r.workspace_id = NEW.workspace_id AND r.system = NEW.role
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END
$$;
CREATE TRIGGER agents_starting_role AFTER INSERT ON agents FOR EACH ROW EXECUTE FUNCTION agents_starting_role();

-- A session may be limited to one role: an agent token that follows it (scope 'role'), or a person's
-- session from a pairing link that asked for the Agent role. legacy marks the tokens made before roles.
ALTER TABLE sessions ADD COLUMN role_id uuid;
ALTER TABLE sessions ADD COLUMN legacy boolean NOT NULL DEFAULT false;
ALTER TABLE sessions ADD CONSTRAINT sessions_role_fkey FOREIGN KEY (workspace_id, role_id) REFERENCES roles(workspace_id, id) ON DELETE RESTRICT;
CREATE INDEX sessions_role_idx ON sessions(workspace_id, role_id) WHERE role_id IS NOT NULL;
ALTER TABLE sessions DROP CONSTRAINT sessions_scope_check;
ALTER TABLE sessions ADD CONSTRAINT sessions_scope_check CHECK (scope IN ('read', 'draft', 'send', 'admin', 'custom', 'role'));
ALTER TABLE sessions ADD CONSTRAINT sessions_role_token CHECK (scope IS DISTINCT FROM 'role' OR (role_id IS NOT NULL AND permissions IS NULL));
ALTER TABLE sessions DROP CONSTRAINT sessions_scope_role_ceiling;
ALTER TABLE sessions ADD CONSTRAINT sessions_scope_role_ceiling
    CHECK (scope IS NULL OR scope IN ('custom', 'role') OR role_ceiling = CASE WHEN scope = 'admin' THEN 'admin' ELSE 'agent' END);

-- An admin's session that asked for the agent role keeps exactly that: the Agent role.
UPDATE sessions s SET role_id = r.id
FROM agents a, roles r
WHERE s.scope IS NULL AND s.role_ceiling = 'agent' AND a.workspace_id = s.workspace_id AND a.id = s.agent_id
    AND a.role = 'admin' AND r.workspace_id = s.workspace_id AND r.system = 'agent';
UPDATE sessions SET legacy = true WHERE scope IS NOT NULL;

-- An unused invite gives the role it was made for.
INSERT INTO invite_roles(workspace_id, link_id, role_id)
SELECT l.workspace_id, l.id, r.id
FROM pairing_links l JOIN roles r ON r.workspace_id = l.workspace_id AND r.system = l.role_ceiling
WHERE l.kind = 'invite' AND l.used_at IS NULL AND l.revoked_at IS NULL;

-- A reply a token wrote for a person to send is a draft above the reply box, written rather than
-- generated by AI. The results and the similarity numbers of AI drafts count only source 'ai'.
ALTER TABLE ai_drafts ADD COLUMN source text NOT NULL DEFAULT 'ai' CHECK (source IN ('ai', 'written'));
ALTER TABLE ai_drafts ADD COLUMN cc text[] CHECK (cc IS NULL OR cardinality(cc) BETWEEN 1 AND 10);

-- Reply drafts still waiting for approval become such drafts, so they stay on their conversation.
INSERT INTO ai_drafts(workspace_id, conversation_id, requested_by_agent_id, requested_by_token_id, status, source, text, cc, ready_at, created_at)
SELECT r.workspace_id, r.conversation_id, r.requested_by_agent_id, r.token_id, 'ready', 'written', r.body->>'body',
    NULLIF(ARRAY(SELECT jsonb_array_elements_text(CASE WHEN jsonb_typeof(r.body->'cc') = 'array' THEN r.body->'cc' ELSE '[]'::jsonb END)), '{}'),
    now(), r.created_at
FROM agent_requests r JOIN conversations c ON c.workspace_id = r.workspace_id AND c.id = r.conversation_id
WHERE r.kind = 'reply_draft' AND r.status = 'pending' AND c.status IN ('open', 'pending')
    AND COALESCE(r.body->>'body', '') <> '';

-- Every request still waiting closes: nothing ran, and nothing will.
UPDATE agent_requests SET status = 'expired', decided_at = now(),
    reason = 'Colliedesk removed agent requests: nothing ran. An agent token now acts at once when it holds the permission.'
WHERE status = 'pending';
