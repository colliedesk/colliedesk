-- The results page (issue #100, docs/internals/results.md): numbers computed in the background,
-- never on a request (results_state says what to compute). results_snapshots holds the numbers;
-- results_members holds, for each number, the conversations it counts, so every number opens the
-- list behind it.
CREATE TABLE results_snapshots (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    id uuid NOT NULL DEFAULT gen_random_uuid(),
    kind text NOT NULL CHECK (kind IN ('baseline', 'current')),
    computed_at timestamptz NOT NULL DEFAULT now(),
    -- The moment the numbers describe: the anchor for a baseline, the compute time otherwise.
    measured_at timestamptz NOT NULL,
    -- Windows, metrics and samples (docs/user/results.md); the API returns it as it is.
    data jsonb NOT NULL,
    PRIMARY KEY (workspace_id, id)
);
CREATE INDEX results_snapshots_kind ON results_snapshots(workspace_id, kind, computed_at DESC);

CREATE TABLE results_members (
    workspace_id uuid NOT NULL,
    snapshot_id uuid NOT NULL,
    metric text NOT NULL,
    conversation_id uuid NOT NULL,
    PRIMARY KEY (workspace_id, snapshot_id, metric, conversation_id),
    FOREIGN KEY (workspace_id, snapshot_id) REFERENCES results_snapshots(workspace_id, id)
        ON DELETE CASCADE
);
-- No foreign key to conversations: a baseline writes tens of thousands of rows at once, and a key
-- (with the index its cascade needs) tripled that write. A deleted conversation's row stays until
-- its snapshot is replaced; the list that opens a number joins conversations, so it never shows.

-- The worker's lease on a workspace, so two servers never compute the same one at once.
ALTER TABLE results_state ADD COLUMN claimed_until timestamptz;

-- The first conversation in Colliedesk (not imported): where the results page starts
-- counting weeks.
CREATE INDEX conversations_live_created ON conversations(workspace_id, created_at)
    WHERE channel NOT IN ('imported', 'problem_report');

SELECT secure_workspace_table('results_snapshots');
SELECT secure_workspace_table('results_members');
