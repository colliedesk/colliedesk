ALTER TABLE messages ADD CONSTRAINT messages_workspace_id_id_key UNIQUE (workspace_id, id);
ALTER TABLE messages ADD COLUMN author_name text;
ALTER TABLE messages ADD COLUMN body_html text;
ALTER TABLE contacts ADD COLUMN organization text;

CREATE TABLE attachments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    message_id uuid NOT NULL,
    filename text NOT NULL CHECK (btrim(filename) <> ''),
    content_type text NOT NULL,
    size bigint NOT NULL CHECK (size >= 0),
    sha256 text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    storage_key text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, message_id) REFERENCES messages(workspace_id, id) ON DELETE CASCADE
);

CREATE TABLE time_entries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL,
    agent_id uuid,
    agent_name text NOT NULL DEFAULT '',
    minutes integer NOT NULL CHECK (minutes >= 0),
    note text NOT NULL DEFAULT '',
    billable boolean NOT NULL,
    executed_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id, conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (agent_id)
);

CREATE TABLE import_items (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    source text NOT NULL,
    entity text NOT NULL,
    source_id text NOT NULL,
    parent_source_id text,
    target_id uuid,
    status text NOT NULL CHECK (status IN ('done', 'failed', 'skipped')),
    error text NOT NULL DEFAULT '',
    source_updated_at timestamptz,
    meta jsonb NOT NULL DEFAULT '{}'::jsonb,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, source, entity, source_id)
);

CREATE INDEX attachments_workspace_message_idx ON attachments(workspace_id, message_id);
CREATE INDEX time_entries_workspace_conversation_idx ON time_entries(workspace_id, conversation_id);
CREATE INDEX import_items_parent_idx ON import_items(workspace_id, source, parent_source_id);
