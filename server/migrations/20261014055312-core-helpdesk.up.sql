CREATE TABLE workspaces (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$'),
    name text NOT NULL CHECK (btrim(name) <> ''),
    timezone text NOT NULL DEFAULT 'UTC',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE agents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    email text NOT NULL,
    display_name text NOT NULL DEFAULT '',
    role text NOT NULL CHECK (role IN ('admin', 'agent')),
    timezone_override text,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, email),
    UNIQUE (workspace_id, id)
);

CREATE TABLE pairing_links (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id uuid,
    email text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('pairing', 'invite')),
    role_ceiling text NOT NULL CHECK (role_ceiling IN ('admin', 'agent')),
    token_hash bytea NOT NULL UNIQUE,
    created_by_agent_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    revoked_at timestamptz,
    FOREIGN KEY (workspace_id, agent_id) REFERENCES agents(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, created_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (created_by_agent_id)
);

CREATE TABLE sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id uuid NOT NULL,
    token_hash bytea NOT NULL UNIQUE,
    role_ceiling text NOT NULL CHECK (role_ceiling IN ('admin', 'agent')),
    label text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    FOREIGN KEY (workspace_id, agent_id) REFERENCES agents(workspace_id, id) ON DELETE CASCADE
);

CREATE TABLE contacts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name text NOT NULL,
    email text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, email),
    UNIQUE (workspace_id, id)
);

CREATE TABLE conversations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    contact_id uuid NOT NULL,
    subject text NOT NULL,
    status text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'pending', 'resolved', 'closed')),
    priority text NOT NULL DEFAULT 'normal' CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    assignee_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, contact_id) REFERENCES contacts(workspace_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, assignee_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (assignee_id)
);

CREATE TABLE messages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL,
    author_agent_id uuid,
    kind text NOT NULL CHECK (kind IN ('incoming', 'outgoing', 'note')),
    body text NOT NULL CHECK (btrim(body) <> ''),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id, conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, author_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (author_agent_id)
);

CREATE TABLE tags (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name text NOT NULL,
    color text NOT NULL DEFAULT '#64748b',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name),
    UNIQUE (workspace_id, id)
);

CREATE TABLE conversation_tags (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL,
    tag_id uuid NOT NULL,
    PRIMARY KEY (workspace_id, conversation_id, tag_id),
    FOREIGN KEY (workspace_id, conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, tag_id) REFERENCES tags(workspace_id, id) ON DELETE CASCADE
);

CREATE INDEX conversations_workspace_updated_idx ON conversations(workspace_id, updated_at DESC);
CREATE INDEX messages_workspace_conversation_idx ON messages(workspace_id, conversation_id, created_at);
CREATE INDEX sessions_token_hash_idx ON sessions(token_hash) WHERE revoked_at IS NULL;
CREATE INDEX pairing_links_token_hash_idx ON pairing_links(token_hash) WHERE revoked_at IS NULL AND used_at IS NULL;
