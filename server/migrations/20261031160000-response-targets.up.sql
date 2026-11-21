-- A workspace's response targets (docs/user/response-targets.md): business hours in a timezone,
-- holidays, first-response and resolution targets by priority in business minutes, how long before
-- a target it warns, the channels they apply to (NULL: every channel) and whether reminder emails
-- go out. One row per workspace; none until an admin sets them (off, no hours, no targets).
CREATE TABLE response_targets (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    enabled boolean NOT NULL DEFAULT false,
    timezone text CHECK (char_length(timezone) BETWEEN 1 AND 64),
    hours jsonb NOT NULL DEFAULT '[]',
    holidays jsonb NOT NULL DEFAULT '[]',
    targets jsonb NOT NULL DEFAULT '{}',
    warn_before_minutes integer NOT NULL DEFAULT 0
        CHECK (warn_before_minutes BETWEEN 0 AND 10080),
    channel_ids uuid[],
    alert_email boolean NOT NULL DEFAULT true,
    -- When they were last turned on: conversations opened before have no target.
    enabled_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by_agent_id uuid,
    FOREIGN KEY (workspace_id, updated_by_agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (updated_by_agent_id)
);

-- Only SELECT, INSERT and UPDATE were granted (no DELETE), but default privileges already give the
-- app role all four; 20261121132937 handles the intent.
SELECT secure_workspace_table('response_targets');
