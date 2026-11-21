-- The names of a Zalo OA's customers as Zalo gives them (their profile, or a group's member list),
-- read once a delivery of theirs was taken: a delivery names its sender by id only. A message
-- parsed after a name is known carries it; looked_up_at keeps a sender Zalo has no name for from
-- being asked again more than once an hour.
CREATE TABLE zalo_users (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id uuid NOT NULL,
    user_id text NOT NULL CHECK (char_length(user_id) BETWEEN 1 AND 100),
    name text CHECK (char_length(name) BETWEEN 1 AND 200),
    looked_up_at timestamptz NOT NULL,
    PRIMARY KEY (workspace_id, channel_id, user_id),
    FOREIGN KEY (workspace_id, channel_id)
        REFERENCES channels(workspace_id, id) ON DELETE CASCADE
);

SELECT secure_workspace_table('zalo_users');
