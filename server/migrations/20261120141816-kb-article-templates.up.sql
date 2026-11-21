-- Article templates: Markdown a team starts new knowledge base articles from. A template is never
-- public: no help center page, search, sitemap, llms.txt or help chat reads this table.
CREATE TABLE kb_templates (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 300),
    body text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 100000),
    created_by_agent_id uuid,
    updated_by_agent_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    deleted_by_agent_id uuid,
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, created_by_agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (created_by_agent_id),
    FOREIGN KEY (workspace_id, updated_by_agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (updated_by_agent_id),
    FOREIGN KEY (workspace_id, deleted_by_agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (deleted_by_agent_id)
);
CREATE UNIQUE INDEX kb_templates_name_idx ON kb_templates(workspace_id, lower(name))
    WHERE deleted_at IS NULL;
-- The daily purge finds rows deleted more than 30 days ago in every workspace.
CREATE INDEX kb_templates_trash_idx ON kb_templates(deleted_at) WHERE deleted_at IS NOT NULL;

CREATE TABLE kb_template_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    template_id uuid NOT NULL,
    version integer NOT NULL CHECK (version >= 1),
    kind text NOT NULL CHECK (kind IN ('create', 'edit', 'delete', 'restore', 'undelete')),
    -- The version a restore copied; set only for kind restore.
    restored_from_version integer CHECK ((kind = 'restore') = (restored_from_version IS NOT NULL)),
    name text NOT NULL,
    description text NOT NULL,
    body text NOT NULL,
    changed_by_agent_id uuid,
    -- The agent token that made the change. The label outlives the token.
    token_id uuid,
    token_label text,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, template_id, version),
    CHECK ((token_id IS NULL) OR (token_label IS NOT NULL)),
    FOREIGN KEY (workspace_id, template_id)
        REFERENCES kb_templates(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, changed_by_agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (changed_by_agent_id),
    FOREIGN KEY (workspace_id, token_id)
        REFERENCES sessions(workspace_id, id) ON DELETE SET NULL (token_id)
);

-- A collection's default template starts every new article in it. Trashing the template clears it
-- (the store does that); purging it for good clears it here.
ALTER TABLE kb_collections ADD COLUMN default_template_id uuid;
ALTER TABLE kb_collections ADD FOREIGN KEY (workspace_id, default_template_id)
    REFERENCES kb_templates(workspace_id, id) ON DELETE SET NULL (default_template_id);

-- When the workspace got its starter templates. Set once, so a starter the team deleted never
-- comes back.
ALTER TABLE workspaces ADD COLUMN kb_starter_templates_at timestamptz;

SELECT secure_workspace_table('kb_templates');
SELECT secure_workspace_table('kb_template_versions');
-- Versions are history: the app adds them and never changes or removes one.
SELECT limit_app_role('kb_template_versions', 'SELECT', 'INSERT');

-- Adds the three starter templates (How-to, Troubleshooting, FAQ) to a workspace that never
-- had them, each with version 1 in its history. A workspace that had them gets nothing, even
-- when the team deleted them since. Workspace creation calls it; so does this migration, for
-- every workspace.
CREATE FUNCTION kb_seed_starter_templates(workspace uuid) RETURNS void LANGUAGE plpgsql AS $fn$
BEGIN
    UPDATE workspaces SET kb_starter_templates_at = now() WHERE id = workspace AND kb_starter_templates_at IS NULL;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    WITH starters(name, description, body) AS (VALUES
        ('How-to', 'Steps to finish one task, with what to check at the end.',
         E'Say in one sentence what the reader can do after these steps.\n\n## Before you start\n\n- What the reader needs (a role, a plan, a setting).\n\n## Steps\n\n1. First step.\n2. Second step.\n3. Third step.\n\n## Check that it worked\n\nWhat the reader sees when the task is done.\n\n## Related articles\n\n- Link to the next task.\n'),
        ('Troubleshooting', 'A problem, how to see its cause, and how to fix it.',
         E'## Problem\n\nWhat the reader sees: the message, the screen or the behaviour.\n\n## Cause\n\nWhy it happens.\n\n## Solution\n\n1. First step.\n2. Second step.\n\n## Still not working?\n\nWhat to send the support team: the time it happened, a screenshot, the address of the page.\n'),
        ('FAQ', 'Short questions and their answers on one topic.',
         E'Questions people ask about this topic.\n\n## Question one?\n\nThe answer in one or two sentences.\n\n## Question two?\n\nThe answer in one or two sentences.\n\n## Question three?\n\nThe answer in one or two sentences.\n')
    ), created AS (
        INSERT INTO kb_templates(workspace_id, name, description, body)
        SELECT workspace, s.name, s.description, s.body FROM starters s
        WHERE NOT EXISTS (SELECT 1 FROM kb_templates t WHERE t.workspace_id = workspace AND t.deleted_at IS NULL AND lower(t.name) = lower(s.name))
        RETURNING workspace_id, id, name, description, body, created_at
    )
    INSERT INTO kb_template_versions(workspace_id, template_id, version, kind, name, description, body, created_at)
    SELECT workspace_id, id, 1, 'create', name, description, body, created_at FROM created;
END
$fn$;

SELECT kb_seed_starter_templates(id) FROM workspaces;
