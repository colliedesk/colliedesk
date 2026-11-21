-- The web form channel: a public website form opens a conversation in the workspace. One row per
-- workspace; no row means the form is off. Off is the default, and a disabled form answers like an
-- unknown workspace. Older images ignore this table.
CREATE TABLE web_form_channels (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    enabled boolean NOT NULL DEFAULT false,
    -- Exact origins (scheme://host[:port]) the browser may call the form from.
    allowed_origins text[] NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Same isolation as workspace_comp_plans.
SELECT secure_workspace_table('web_form_channels');
