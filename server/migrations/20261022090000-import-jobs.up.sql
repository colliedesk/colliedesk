-- In-app imports (#97). A row is the durable state of one import (or undo) the server runs in the
-- background: what a person or an AI agent sees, and what a worker leases. The Freshdesk API key is
-- held sealed (tokenseal, MAILBOX_TOKEN_KEYS) only while the job can still use it, and erased in the
-- update that ends the job. See docs/internals/freshdesk-import.md, "In-app import (#97): the job".
CREATE TABLE import_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('import', 'undo')),
    source text NOT NULL CHECK (source IN ('freshdesk')),
    -- The Freshdesk account host, for example acme.freshdesk.com.
    domain text NOT NULL CHECK (char_length(domain) BETWEEN 1 AND 255),
    -- The ledger source (freshdesk:account:<id>), known after the preview.
    account_source text,
    -- The import an undo job removes.
    undo_of uuid,
    state text NOT NULL CHECK (state IN ('previewing', 'previewed', 'queued', 'running', 'retry_wait', 'waiting_key',
        'finished', 'failed', 'cancelled', 'discarded')),
    phase text NOT NULL CHECK (phase IN ('preview', 'recent', 'full', 'undo')),
    options jsonb NOT NULL DEFAULT '{}',
    preview jsonb,
    progress jsonb,
    interim_report jsonb,
    report jsonb,
    report_markdown text,
    status text CHECK (status IN ('OK', 'FAILED', 'UNVERIFIED')),
    error_code text,
    error_message text,
    sealed_key bytea,
    sealed_key_id text,
    -- The process holding the key in memory when the server has no key ring.
    owner_instance text,
    claim_token uuid,
    lease_until timestamptz,
    next_run_at timestamptz NOT NULL DEFAULT now(),
    attempts integer NOT NULL DEFAULT 0,
    attempts_without_progress integer NOT NULL DEFAULT 0,
    -- Start of the current run of retry_wait; the job fails after 24 hours of it.
    retrying_since timestamptz,
    full_retries integer NOT NULL DEFAULT 0,
    cancel_requested_at timestamptz,
    created_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    finished_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, created_by) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (created_by),
    FOREIGN KEY (workspace_id, undo_of) REFERENCES import_jobs(workspace_id, id) ON DELETE CASCADE,
    CHECK ((kind = 'undo') = (undo_of IS NOT NULL)),
    CHECK ((kind = 'undo') = (phase = 'undo')),
    CHECK ((sealed_key IS NULL) = (sealed_key_id IS NULL)),
    -- A job that has ended holds no key.
    CHECK (sealed_key IS NULL OR state IN ('previewing', 'previewed', 'queued', 'running', 'retry_wait'))
);

-- One import or undo at a time per workspace.
CREATE UNIQUE INDEX import_jobs_one_active ON import_jobs(workspace_id)
    WHERE state IN ('queued', 'running', 'retry_wait', 'waiting_key');
CREATE INDEX import_jobs_due ON import_jobs(next_run_at)
    WHERE state IN ('previewing', 'queued', 'running', 'retry_wait');
CREATE INDEX import_jobs_workspace ON import_jobs(workspace_id, created_at DESC);

DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE import_jobs ENABLE ROW LEVEL SECURITY;
    ALTER TABLE import_jobs FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON import_jobs FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON import_jobs FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON import_jobs TO %I', app_role);
END
$$;
