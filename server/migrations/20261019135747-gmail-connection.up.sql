-- Gmail connected for sending (issue #95, phase A). One connection per workspace, tied to its
-- email channel: disconnecting the channel removes it. The refresh token is sealed with AES-256-GCM
-- by the server (tokenseal, MAILBOX_TOKEN_KEYS) with the workspace id as additional data; the
-- database never holds it in clear. status 'failed' keeps the row (and so the address and the
-- reason) when Google stops honouring the grant, so replies fail visibly instead of leaving
-- through another transport.
CREATE TABLE mailbox_connections (
    workspace_id uuid PRIMARY KEY REFERENCES email_channels(workspace_id) ON DELETE CASCADE,
    provider text NOT NULL CHECK (provider IN ('google')),
    address text NOT NULL
        CHECK (address = lower(address) AND address LIKE '_%@_%' AND char_length(address) <= 254),
    scopes text NOT NULL,
    token_key_id text NOT NULL,
    refresh_token_sealed bytea NOT NULL,
    status text NOT NULL DEFAULT 'connected' CHECK (status IN ('connected', 'failed')),
    error_code text CHECK (error_code IN ('revoked', 'permission')),
    failed_at timestamptz,
    connected_by_agent_id uuid REFERENCES agents(id) ON DELETE SET NULL,
    connected_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((status = 'failed') = (error_code IS NOT NULL AND failed_at IS NOT NULL))
);

-- An OAuth consent in flight: the hash of its state, who started it and the PKCE verifier. Used
-- once, within 10 minutes, by the same person in the same workspace.
CREATE TABLE mailbox_oauth_states (
    state_hash bytea PRIMARY KEY CHECK (octet_length(state_hash) = 32),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    provider text NOT NULL CHECK (provider IN ('google')),
    code_verifier text NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX mailbox_oauth_states_workspace_idx ON mailbox_oauth_states(workspace_id, expires_at);

-- Which transport a queued reply was accepted for. A reply accepted for Gmail never leaves
-- through SMTP: when the connection is gone or broken it fails with the reason, and retrying
-- chooses again.
ALTER TABLE email_outbox ADD COLUMN transport text NOT NULL DEFAULT 'smtp'
    CHECK (transport IN ('smtp', 'gmail'));

-- Same isolation as saved_replies.
SELECT secure_workspace_table('mailbox_connections');
SELECT secure_workspace_table('mailbox_oauth_states');
