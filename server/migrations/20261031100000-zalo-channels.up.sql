-- Zalo Official Account channels (docs/user/zalo.md): a channel is one OA
-- (channels.external_account is its oa_id, unique on the server, so the app's one webhook routes
-- by it). What is not a secret is here: the Zalo app it is reached through, the team's Zalo user
-- ids (their messages in a group are the team's, never a customer's request) and the organization
-- each group stands for. Its secrets (the app's and the OA's keys, the access and refresh tokens)
-- are channel_credentials.
INSERT INTO channel_types (type, connectable) VALUES ('zalo', true);

CREATE TABLE zalo_channels (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id uuid NOT NULL,
    app_id text NOT NULL CHECK (app_id ~ '^[0-9]{1,40}$'),
    staff_user_ids text[] NOT NULL DEFAULT '{}' CHECK (cardinality(staff_user_ids) <= 200),
    -- Group id to the organization its members are customers of.
    groups jsonb NOT NULL DEFAULT '{}'
        CHECK (jsonb_typeof(groups) = 'object' AND octet_length(groups::text) <= 65536),
    -- The last delivery whose signature did not match, so the channel says why events are lost.
    signature_failed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, channel_id),
    FOREIGN KEY (workspace_id, channel_id)
        REFERENCES channels(workspace_id, id) ON DELETE CASCADE
);

SELECT secure_workspace_table('zalo_channels');
