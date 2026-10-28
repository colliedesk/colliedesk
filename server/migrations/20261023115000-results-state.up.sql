-- What the results page (issue #100, docs/internals/results.md) computes for a workspace. The baseline
-- is measured from one import: its ledger source, when its first full run finished (the anchor, which
-- a later run never moves; ticket stats read after it are frozen), and the reconcile status of its
-- latest run (NULL for an import made before results existed). All NULL without a baseline.
CREATE TABLE results_state (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    baseline_source text,
    baseline_anchor_at timestamptz,
    baseline_import_status text CHECK (baseline_import_status IN ('OK', 'FAILED', 'UNVERIFIED')),
    -- An import of the baseline's source ran or was undone: compute the baseline again.
    baseline_dirty boolean NOT NULL DEFAULT false,
    -- The last time the current numbers were computed, and what they were computed from.
    computed_at timestamptz,
    inputs jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- A compute that failed: its message and when, shown to admins; cleared by the next success.
    error text,
    error_at timestamptz,
    CHECK ((baseline_source IS NULL) = (baseline_anchor_at IS NULL)),
    CHECK ((error IS NULL) = (error_at IS NULL))
);

DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE results_state ENABLE ROW LEVEL SECURITY;
    ALTER TABLE results_state FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON results_state FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON results_state FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON results_state TO %I', app_role);
END
$$;
