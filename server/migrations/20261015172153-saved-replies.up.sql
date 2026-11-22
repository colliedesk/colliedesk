-- Saved replies: text the whole workspace shares and any agent inserts into the reply box.
-- The title is unique per workspace ignoring case; the limits on length and count are also
-- enforced by the API. Older images ignore this table.
CREATE TABLE saved_replies (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    body text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 20000),
    created_by_agent_id uuid REFERENCES agents(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX saved_replies_workspace_title_idx ON saved_replies(workspace_id, lower(title));

-- Same isolation as web_form_channels.
SELECT secure_workspace_table('saved_replies');
