-- Intake's offline evaluations (docs/internals/channels.md "Readiness"): an operator runs the AI decider
-- on a labelled dataset with `collie intake-eval` and records the counts. A run counts towards a
-- scope's readiness only for the model and prompt version it ran on; the newest run of a dataset
-- replaces its older ones.
CREATE TABLE intake_eval_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id uuid NOT NULL,
    scope text NOT NULL DEFAULT '' CHECK (scope ~ '^[a-z_]{0,40}$'),
    model text NOT NULL CHECK (char_length(model) BETWEEN 1 AND 200),
    prompt_version text NOT NULL CHECK (char_length(prompt_version) BETWEEN 1 AND 80),
    dataset_sha256 text NOT NULL CHECK (dataset_sha256 ~ '^[0-9a-f]{64}$'),
    -- Per action (create, attach, ignore): labels, correct, severe, light.
    counts jsonb NOT NULL CHECK (jsonb_typeof(counts) = 'object' AND octet_length(counts::text) <= 4096),
    run_by_agent_id uuid,
    run_by_token_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, run_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (run_by_agent_id)
);
CREATE INDEX intake_eval_runs_scope ON intake_eval_runs(workspace_id, channel_id, scope, model, prompt_version, created_at DESC);

DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE intake_eval_runs ENABLE ROW LEVEL SECURITY;
    ALTER TABLE intake_eval_runs FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON intake_eval_runs FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON intake_eval_runs FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON intake_eval_runs TO %I', app_role);
END
$$;

-- Readiness finds the person's decision that followed or corrected each AI decision.
CREATE INDEX intake_decisions_follows ON intake_decisions(workspace_id, follows_decision_id) WHERE follows_decision_id IS NOT NULL;
CREATE INDEX intake_decisions_corrects ON intake_decisions(workspace_id, corrects_decision_id) WHERE corrects_decision_id IS NOT NULL;
