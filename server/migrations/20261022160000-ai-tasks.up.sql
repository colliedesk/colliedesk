-- The AI task queue (#89). A task is one piece of AI work the server runs in the background (a triage
-- suggestion, a reply draft), so no screen waits for a model. A worker of `collie serve` leases due
-- tasks like import jobs, calls the provider through the metered layer and records the outcome. A
-- restored workspace never gets its tasks back (restore-workspace.sh): they would spend again.
CREATE TABLE ai_tasks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('triage', 'draft')),
    conversation_id uuid,
    -- Started by the server (a new ticket), not by a person: counted against the automatic limits.
    automatic boolean NOT NULL DEFAULT false,
    requested_by_agent_id uuid,
    requested_by_token_id uuid,
    state text NOT NULL CHECK (state IN ('queued', 'running', 'done', 'failed', 'cancelled')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_run_at timestamptz NOT NULL DEFAULT now(),
    claim_token uuid,
    lease_until timestamptz,
    error_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    finished_at timestamptz,
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, requested_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (requested_by_agent_id),
    CHECK ((state IN ('done', 'failed', 'cancelled')) = (finished_at IS NOT NULL)),
    CHECK (state <> 'failed' AND state <> 'cancelled' OR error_code IS NOT NULL),
    CHECK ((claim_token IS NULL) = (lease_until IS NULL))
);
CREATE INDEX ai_tasks_due ON ai_tasks(next_run_at) WHERE state IN ('queued', 'running');
CREATE INDEX ai_tasks_workspace ON ai_tasks(workspace_id, created_at DESC);
-- The automatic limits count the automatic tasks started today, on every tick and every GET /ai.
CREATE INDEX ai_tasks_automatic_started ON ai_tasks(workspace_id, started_at) WHERE automatic;
CREATE INDEX ai_tasks_conversation ON ai_tasks(workspace_id, conversation_id, kind, created_at DESC) WHERE conversation_id IS NOT NULL;

-- Each usage row names its task, whether the call was automatic, and, for a call the server's
-- provider paid, its cost at the prices of the day it ran (micro-dollars), so a later price change
-- never rewrites history or the month's allowance.
ALTER TABLE ai_usage ADD COLUMN task_id uuid;
ALTER TABLE ai_usage ADD COLUMN automatic boolean NOT NULL DEFAULT false;
ALTER TABLE ai_usage ADD COLUMN cost_micro_usd bigint CHECK (cost_micro_usd >= 0);
ALTER TABLE ai_usage ADD FOREIGN KEY (workspace_id, task_id) REFERENCES ai_tasks(workspace_id, id) ON DELETE SET NULL (task_id);
CREATE INDEX ai_usage_task ON ai_usage(workspace_id, task_id) WHERE task_id IS NOT NULL;
-- The worker's sweep looks for calls left started on every tick; the table only grows.
CREATE INDEX ai_usage_started ON ai_usage(started_at) WHERE status = 'started';

-- How many automatic tasks a workspace runs a day (UTC) and a minute; no row is the defaults (300, 30).
-- They bound what a flood of tickets can spend, on the server's provider and on a workspace's own key.
CREATE TABLE ai_automatic_limits (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    per_day integer NOT NULL CHECK (per_day BETWEEN 0 AND 10000),
    per_minute integer NOT NULL CHECK (per_minute BETWEEN 0 AND 1000),
    updated_by uuid,
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id, updated_by) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (updated_by)
);

-- Changing the automatic limits is part of the AI settings history.
ALTER TABLE ai_settings_events DROP CONSTRAINT ai_settings_events_kind_check;
ALTER TABLE ai_settings_events ADD CONSTRAINT ai_settings_events_kind_check CHECK (kind IN ('provider_added', 'provider_changed',
    'provider_key_changed', 'provider_removed', 'slot_changed', 'automatic_limits_changed'));

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
    FOREACH name IN ARRAY ARRAY['ai_tasks', 'ai_automatic_limits'] LOOP
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
