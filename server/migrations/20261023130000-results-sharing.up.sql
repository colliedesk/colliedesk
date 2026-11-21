-- Sharing the results page (issue #100, docs/internals/results.md): a revocable link that shows the
-- totals to someone without a seat, and the weekly email to admins.
--
-- A share link is stored only as the SHA-256 of its token, like pairing links. At most one is
-- active per workspace: creating one turns the old one off.
CREATE TABLE results_share_links (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    id uuid NOT NULL DEFAULT gen_random_uuid(),
    token_hash text NOT NULL UNIQUE,
    created_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    revoked_by uuid,
    last_viewed_at timestamptz,
    PRIMARY KEY (workspace_id, id),
    FOREIGN KEY (workspace_id, created_by)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (created_by),
    FOREIGN KEY (workspace_id, revoked_by)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (revoked_by)
);
CREATE UNIQUE INDEX results_share_links_active ON results_share_links(workspace_id)
    WHERE revoked_at IS NULL;

-- Each admin chooses whether to get the weekly email; on by default.
ALTER TABLE agents ADD COLUMN results_email boolean NOT NULL DEFAULT true;
-- The week (its Monday 00:00 in the workspace's timezone, as an instant) whose email was claimed,
-- so two servers never send one twice.
ALTER TABLE results_state ADD COLUMN weekly_sent_week timestamptz;

SELECT secure_workspace_table('results_share_links');
