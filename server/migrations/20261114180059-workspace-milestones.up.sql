-- A workspace's lifecycle milestones (docs/internals/workspace-milestones.md): one row per fact, written by
-- the server in the transaction that stores the fact itself. The log is append-only and outlives its
-- workspace: no foreign key, and a trigger refuses UPDATE, DELETE and TRUNCATE for every role. detail
-- holds ids and counts only, never a name, an address or message text.
CREATE TABLE workspace_events (
    id bigserial PRIMARY KEY,
    workspace_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('signed_up', 'first_customer_message', 'import_finished', 'first_reply',
        'teammate_invited', 'trial_ended', 'paid', 'cancelled')),
    -- When the fact happened; recorded_at is when this row was written.
    at timestamptz NOT NULL,
    source text NOT NULL CHECK (source IN ('app', 'backfill')),
    recorded_at timestamptz NOT NULL DEFAULT now(),
    detail jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(detail) = 'object')
);
-- Every kind but paid and cancelled happens once per workspace: the first occurrence wins.
CREATE UNIQUE INDEX workspace_events_first ON workspace_events(workspace_id, kind) WHERE kind NOT IN ('paid', 'cancelled');
CREATE INDEX workspace_events_workspace_kind ON workspace_events(workspace_id, kind, at);

CREATE FUNCTION workspace_events_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'workspace_events is append-only: % is refused', TG_OP;
END
$$;
CREATE TRIGGER workspace_events_append_only BEFORE UPDATE OR DELETE ON workspace_events
    FOR EACH ROW EXECUTE FUNCTION workspace_events_append_only();
CREATE TRIGGER workspace_events_no_truncate BEFORE TRUNCATE ON workspace_events
    FOR EACH STATEMENT EXECUTE FUNCTION workspace_events_append_only();

-- One row per workspace with any event. paid_at and cancelled_at are the latest occurrence. The view runs
-- with the reader's rights, so a workspace's transaction sees its own row only.
CREATE VIEW workspace_milestones WITH (security_invoker = true) AS
SELECT workspace_id,
    min(at) FILTER (WHERE kind = 'signed_up') AS signed_up_at,
    min(at) FILTER (WHERE kind = 'first_customer_message') AS first_customer_message_at,
    min(at) FILTER (WHERE kind = 'import_finished') AS import_finished_at,
    min(at) FILTER (WHERE kind = 'first_reply') AS first_reply_at,
    min(at) FILTER (WHERE kind = 'teammate_invited') AS teammate_invited_at,
    min(at) FILTER (WHERE kind = 'trial_ended') AS trial_ended_at,
    max(at) FILTER (WHERE kind = 'paid') AS paid_at,
    max(at) FILTER (WHERE kind = 'cancelled') AS cancelled_at
FROM workspace_events
GROUP BY workspace_id;

-- Same isolation as the other workspace tables. The schema's default privileges grant UPDATE and DELETE on
-- every new table; the app role keeps only SELECT and INSERT, and SELECT on the view.
DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE workspace_events ENABLE ROW LEVEL SECURITY;
    ALTER TABLE workspace_events FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON workspace_events FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON workspace_events FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('REVOKE ALL ON workspace_events, workspace_milestones FROM %I', app_role);
    EXECUTE format('GRANT SELECT, INSERT ON workspace_events TO %I', app_role);
    EXECUTE format('GRANT USAGE ON SEQUENCE workspace_events_id_seq TO %I', app_role);
    EXECUTE format('GRANT SELECT ON workspace_milestones TO %I', app_role);
END
$$;

-- Backfill from the rows the facts left, with the same exclusions as the server (internal/milestones).
-- trial_ended is not backfilled here: whether billing is on is server configuration the database does not
-- hold, and a self-hosted trial never ends. The retention pass of a server with billing records every
-- trial that already ended, at its end, on its first run.
INSERT INTO workspace_events(workspace_id, kind, at, source)
SELECT id, 'signed_up', created_at, 'backfill' FROM workspaces WHERE created_at IS NOT NULL ORDER BY created_at, id;

-- A live message from a customer: not imported, through a channel, not refused by the sender check, not
-- Gmail's forwarding confirmation, not from one of the workspace's own agents (the test message a person
-- sends themselves comes from their own address), and not in a conversation marked spam.
INSERT INTO workspace_events(workspace_id, kind, at, source, detail)
SELECT DISTINCT ON (m.workspace_id) m.workspace_id, 'first_customer_message', m.created_at, 'backfill',
    jsonb_build_object('message_id', m.id, 'conversation_id', m.conversation_id, 'channel_id', m.channel_id, 'channel_type', ch.type)
FROM messages m
JOIN channels ch ON ch.workspace_id = m.workspace_id AND ch.id = m.channel_id
WHERE m.kind = 'incoming' AND m.origin IS NULL AND m.created_at IS NOT NULL
    AND m.email_meta->>'authentication' IS DISTINCT FROM 'fail'
    AND lower(m.email_meta->>'from') IS DISTINCT FROM 'forwarding-noreply@google.com'
    AND NOT EXISTS (SELECT 1 FROM agents a WHERE a.workspace_id = m.workspace_id AND a.email = lower(m.email_meta->>'from'))
    AND NOT EXISTS (SELECT 1 FROM conversations c WHERE c.workspace_id = m.workspace_id AND c.id = m.conversation_id AND c.status = 'spam')
ORDER BY m.workspace_id, m.created_at, m.id;

-- An import that finished OK: an in-app job, or the first full run of any import (the command line's too),
-- which the results page anchors on.
INSERT INTO workspace_events(workspace_id, kind, at, source, detail)
SELECT DISTINCT ON (f.workspace_id) f.workspace_id, 'import_finished', f.at, 'backfill', jsonb_build_object('importer', f.importer)
FROM (
    SELECT workspace_id, finished_at AS at, source AS importer FROM import_jobs
        WHERE kind = 'import' AND state = 'finished' AND status = 'OK' AND finished_at IS NOT NULL
    UNION ALL
    SELECT workspace_id, baseline_anchor_at, split_part(baseline_source, ':', 1) FROM results_state
        WHERE baseline_import_status = 'OK' AND baseline_anchor_at IS NOT NULL AND baseline_source IS NOT NULL
) f
ORDER BY f.workspace_id, f.at;

-- A reply a person or an agent token wrote in Colliedesk: not imported, not the channel's own notice, and
-- not to a conversation whose customer is one of the workspace's agents (the answer to a test message).
INSERT INTO workspace_events(workspace_id, kind, at, source, detail)
SELECT DISTINCT ON (m.workspace_id) m.workspace_id, 'first_reply', m.created_at, 'backfill',
    jsonb_build_object('message_id', m.id, 'conversation_id', m.conversation_id)
FROM messages m
WHERE m.kind = 'outgoing' AND m.origin IS NULL AND m.notice_kind IS NULL AND m.created_at IS NOT NULL
    AND (m.author_agent_id IS NOT NULL OR m.agent_token_id IS NOT NULL)
    AND NOT EXISTS (SELECT 1 FROM conversations c
        JOIN contacts k ON k.workspace_id = c.workspace_id AND k.id = c.contact_id
        JOIN agents a ON a.workspace_id = c.workspace_id AND a.email = lower(k.email)
        WHERE c.workspace_id = m.workspace_id AND c.id = m.conversation_id)
ORDER BY m.workspace_id, m.created_at, m.id;

-- An invite to someone who was not a member yet (an invite to a member names them in agent_id).
INSERT INTO workspace_events(workspace_id, kind, at, source, detail)
SELECT DISTINCT ON (workspace_id) workspace_id, 'teammate_invited', created_at, 'backfill', jsonb_build_object('invite_id', id)
FROM pairing_links
WHERE kind = 'invite' AND agent_id IS NULL AND created_at IS NOT NULL
ORDER BY workspace_id, created_at, id;

-- Each subscription Polar made active, and each cancellation it reported, as its webhook arrived.
INSERT INTO workspace_events(workspace_id, kind, at, source)
SELECT workspace_id, CASE event_type WHEN 'subscription.active' THEN 'paid' ELSE 'cancelled' END, received_at, 'backfill'
FROM billing_webhook_events
WHERE event_type IN ('subscription.active', 'subscription.canceled') AND received_at IS NOT NULL
ORDER BY workspace_id, received_at, webhook_id;
