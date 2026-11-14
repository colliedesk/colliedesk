-- Macros: a reply template plus actions (status, priority, assignee, tags) an agent applies in one
-- step. A personal macro belongs to one agent; a shared one to the workspace. A delete waits 30 days
-- in the trash (deleted_at). An application records what a macro changed on one conversation so it
-- can be undone. Groups do not exist, so a macro cannot assign to a group.
CREATE TABLE macros (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    scope text NOT NULL CHECK (scope IN ('personal', 'shared')),
    owner_agent_id uuid,
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200 AND btrim(title) <> ''),
    body text NOT NULL DEFAULT '' CHECK (char_length(body) <= 20000),
    actions jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(actions) = 'object' AND octet_length(actions::text) <= 8192),
    created_by_agent_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    deleted_by_agent_id uuid,
    CHECK ((scope = 'personal') = (owner_agent_id IS NOT NULL)),
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, owner_agent_id) REFERENCES agents(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, created_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (created_by_agent_id),
    FOREIGN KEY (workspace_id, deleted_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (deleted_by_agent_id)
);

CREATE UNIQUE INDEX macros_personal_title_idx ON macros(workspace_id, owner_agent_id, lower(title))
    WHERE deleted_at IS NULL AND scope = 'personal';
CREATE UNIQUE INDEX macros_shared_title_idx ON macros(workspace_id, lower(title))
    WHERE deleted_at IS NULL AND scope = 'shared';
CREATE INDEX macros_trash_idx ON macros(workspace_id, deleted_at) WHERE deleted_at IS NOT NULL;

-- What one apply changed, so undo can put those fields back. macro_id becomes null when the macro
-- is purged; the title stays so the history still names it.
CREATE TABLE macro_applications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL,
    macro_id uuid,
    macro_title text NOT NULL CHECK (char_length(macro_title) BETWEEN 1 AND 200),
    agent_id uuid,
    rendered_body text NOT NULL DEFAULT '' CHECK (char_length(rendered_body) <= 20000),
    before jsonb NOT NULL CHECK (jsonb_typeof(before) = 'object'),
    applied jsonb NOT NULL CHECK (jsonb_typeof(applied) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    undone_at timestamptz,
    undone_by_agent_id uuid,
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, macro_id) REFERENCES macros(workspace_id, id) ON DELETE SET NULL (macro_id),
    FOREIGN KEY (workspace_id, agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (agent_id),
    FOREIGN KEY (workspace_id, undone_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (undone_by_agent_id)
);
CREATE INDEX macro_applications_conversation_idx ON macro_applications(workspace_id, conversation_id, created_at DESC);

DO $$
DECLARE
    app_role text;
    name text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    FOREACH name IN ARRAY ARRAY['macros', 'macro_applications'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', name);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', name);
        EXECUTE format($policy$CREATE POLICY workspace_isolation ON %2$I FOR ALL TO %1$I
            USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
            WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role, name);
        EXECUTE format($policy$CREATE POLICY privileged_access ON %2$I FOR ALL TO %1$I
            USING ((SELECT current_setting('app.privileged', true) = 'on'))
            WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user, name);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON %I TO %I', name, app_role);
    END LOOP;
END
$$;

-- Macros are how every agent answers, the same as saved replies, so the built-in Agent role gains
-- them. A workspace that already changed that role keeps its list. Reset to default includes them
-- (auth.AgentRolePermissions, which stays equal to the function below).
SELECT set_config('app.privileged', 'on', true);
UPDATE roles SET permissions = permissions || ARRAY['macros:read', 'macros:create', 'macros:update', 'macros:delete']::text[], updated_at = now()
WHERE system = 'agent'
  AND permissions = ARRAY['conversations:read', 'conversations:update', 'messages:read', 'messages:note', 'messages:send',
        'ai_assist:read', 'ai_assist:create', 'contacts:read', 'contacts:create', 'contacts:update', 'tags:read',
        'tags:create', 'tags:update', 'tags:delete', 'saved_replies:read', 'saved_replies:create',
        'saved_replies:update', 'saved_replies:delete', 'saved_views:read', 'saved_views:create',
        'saved_views:update', 'saved_views:delete', 'kb_articles:read', 'kb_articles:create', 'kb_articles:update',
        'kb_articles:delete', 'kb_articles:publish', 'kb_media:read', 'kb_media:create', 'kb_media:update',
        'kb_media:delete', 'help_center:read', 'results:read', 'profile:update', 'team:read', 'channels:read',
        'ai_settings:read', 'autofix:read', 'autofix:update', 'autofix:send', 'autofix_settings:read',
        'team:create_tokens']::text[];

CREATE OR REPLACE FUNCTION colliedesk_agent_role_permissions() RETURNS text[] LANGUAGE sql IMMUTABLE AS $$
    SELECT ARRAY['conversations:read', 'conversations:update', 'messages:read', 'messages:note', 'messages:send',
        'ai_assist:read', 'ai_assist:create', 'contacts:read', 'contacts:create', 'contacts:update', 'tags:read',
        'tags:create', 'tags:update', 'tags:delete', 'saved_replies:read', 'saved_replies:create',
        'saved_replies:update', 'saved_replies:delete', 'saved_views:read', 'saved_views:create',
        'saved_views:update', 'saved_views:delete', 'kb_articles:read', 'kb_articles:create', 'kb_articles:update',
        'kb_articles:delete', 'kb_articles:publish', 'kb_media:read', 'kb_media:create', 'kb_media:update',
        'kb_media:delete', 'help_center:read', 'results:read', 'profile:update', 'team:read', 'channels:read',
        'ai_settings:read', 'autofix:read', 'autofix:update', 'autofix:send', 'autofix_settings:read',
        'team:create_tokens', 'macros:read', 'macros:create', 'macros:update', 'macros:delete']::text[]
$$;
