-- The results page (issue #100, docs/internals/results.md): numbers computed in the background, never
-- on a request (results_state says what to compute). results_snapshots holds the numbers;
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
    FOREIGN KEY (workspace_id, snapshot_id) REFERENCES results_snapshots(workspace_id, id) ON DELETE CASCADE
);
-- No foreign key to conversations: a baseline writes tens of thousands of rows at once, and a key
-- (with the index its cascade needs) tripled that write. A deleted conversation's row stays until
-- its snapshot is replaced; the list that opens a number joins conversations, so it never shows.

-- The worker's lease on a workspace, so two servers never compute the same one at once.
ALTER TABLE results_state ADD COLUMN claimed_until timestamptz;

-- The first conversation in Colliedesk (not imported): where the results page starts counting weeks.
CREATE INDEX conversations_live_created ON conversations(workspace_id, created_at) WHERE channel NOT IN ('imported', 'problem_report');

DO $$
DECLARE
    app_role text;
    name text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    FOREACH name IN ARRAY ARRAY['results_snapshots', 'results_members'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', name);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', name);
        EXECUTE format($policy$CREATE POLICY workspace_isolation ON %1$I FOR ALL TO %2$I
            USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
            WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, name, app_role);
        EXECUTE format($policy$CREATE POLICY privileged_access ON %1$I FOR ALL TO %2$I
            USING ((SELECT current_setting('app.privileged', true) = 'on'))
            WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, name, current_user);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON %I TO %I', name, app_role);
    END LOOP;
END
$$;
