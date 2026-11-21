-- A trial offer is a one-time link an operator sends to a team whose trial ran out: an admin of
-- that workspace who accepts it gets 14 more days from that moment. Only a hash of the link is
-- stored, as for invites. An offer is open until it is accepted, revoked or past expires_at.
CREATE TABLE trial_offers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    accepted_at timestamptz,
    accepted_by_agent_id uuid REFERENCES agents(id) ON DELETE SET NULL,
    accepted_by text,
    revoked_at timestamptz,
    revoked_by text,
    CHECK (expires_at > created_at),
    CHECK (accepted_at IS NULL OR revoked_at IS NULL)
);

CREATE INDEX trial_offers_workspace_idx ON trial_offers(workspace_id, created_at);

-- Same isolation as workspace_trial_changes.
SELECT secure_workspace_table('trial_offers');
