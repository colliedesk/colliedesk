-- A workspace's lifecycle milestones (docs/internals/workspace-milestones.md): one row per fact,
-- written by the server in the transaction that stores the fact itself. The log is append-only and
-- outlives its workspace: no foreign key, and a trigger refuses UPDATE, DELETE and TRUNCATE for
-- every role. detail holds ids and counts only, never a name, an address or message text.
CREATE TABLE workspace_events (
    id bigserial PRIMARY KEY,
    workspace_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN (
        'signed_up', 'first_customer_message', 'import_finished', 'first_reply',
        'teammate_invited', 'trial_ended', 'paid', 'cancelled'
    )),
    -- When the fact happened; recorded_at is when this row was written.
    at timestamptz NOT NULL,
    source text NOT NULL CHECK (source IN ('app', 'backfill')),
    recorded_at timestamptz NOT NULL DEFAULT now(),
    detail jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(detail) = 'object')
);
-- Every kind but paid and cancelled happens once per workspace: the first occurrence wins.
CREATE UNIQUE INDEX workspace_events_first ON workspace_events(workspace_id, kind)
    WHERE kind NOT IN ('paid', 'cancelled');
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

-- One row per workspace with any event. paid_at and cancelled_at are the latest occurrence. The
-- view runs with the reader's rights, so a workspace's transaction sees its own row only.
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

-- Same isolation as the other workspace tables. The schema's default privileges grant UPDATE and
-- DELETE on every new table; the app role keeps only SELECT and INSERT, and SELECT on the view.
SELECT secure_workspace_table('workspace_events');
SELECT limit_app_role('workspace_events', 'SELECT', 'INSERT');
SELECT limit_app_role('workspace_milestones', 'SELECT');
-- The helper also grants SELECT on the id sequence; the app role only needs USAGE to insert.
DO $$
BEGIN
    EXECUTE format('REVOKE SELECT ON SEQUENCE workspace_events_id_seq FROM %I', app_role());
END
$$;

-- Backfill from the rows the facts left, with the same exclusions as the server
-- (internal/milestones). trial_ended is not backfilled here: whether billing is on is server
-- configuration the database does not hold, and a self-hosted trial never ends. The retention pass
-- of a server with billing records every trial that already ended, at its end, on its first run.
INSERT INTO workspace_events(workspace_id, kind, at, source)
SELECT id, 'signed_up', created_at, 'backfill' FROM workspaces
WHERE created_at IS NOT NULL
ORDER BY created_at, id;

-- A live message from a customer: not imported, through a channel, not refused by the sender check,
-- not Gmail's forwarding confirmation, and not from one of the workspace's own agents (the test
-- message a person sends themselves comes from their own address). The server also leaves out a
-- message that arrives in a conversation already marked spam; the stored rows do not say what a
-- conversation's status was when the message arrived, and its status now can be a later decision,
-- so the backfill does not use it.
INSERT INTO workspace_events(workspace_id, kind, at, source, detail)
SELECT DISTINCT ON (m.workspace_id) m.workspace_id, 'first_customer_message', m.created_at,
    'backfill',
    jsonb_build_object(
        'message_id', m.id,
        'conversation_id', m.conversation_id,
        'channel_id', m.channel_id,
        'channel_type', ch.type
    )
FROM messages m
JOIN channels ch ON ch.workspace_id = m.workspace_id AND ch.id = m.channel_id
WHERE m.kind = 'incoming' AND m.origin IS NULL AND m.created_at IS NOT NULL
    AND m.email_meta->>'authentication' IS DISTINCT FROM 'fail'
    AND lower(m.email_meta->>'from') IS DISTINCT FROM 'forwarding-noreply@google.com'
    AND NOT EXISTS (
        SELECT 1 FROM agents a
        WHERE a.workspace_id = m.workspace_id AND a.email = lower(m.email_meta->>'from')
    )
ORDER BY m.workspace_id, m.created_at, m.id;

-- The first full import run that finished OK. An in-app job keeps the time its run finished. An
-- import run from the command line keeps no run record, only the results baseline: its anchor is
-- the finish time of the first full run of that source, which is the first OK finish unless an
-- earlier run failed. The row says "approximate" then, and it is written only where no OK job
-- exists and the baseline's last run is OK.
INSERT INTO workspace_events(workspace_id, kind, at, source, detail)
SELECT DISTINCT ON (f.workspace_id) f.workspace_id, 'import_finished', f.at, 'backfill', f.detail
FROM (
    SELECT workspace_id, finished_at AS at, jsonb_build_object('importer', source) AS detail
        FROM import_jobs
        WHERE kind = 'import' AND state = 'finished' AND status = 'OK' AND finished_at IS NOT NULL
    UNION ALL
    SELECT r.workspace_id, r.baseline_anchor_at,
        jsonb_build_object(
            'importer', split_part(r.baseline_source, ':', 1),
            'approximate', true
        )
        FROM results_state r
        WHERE r.baseline_import_status = 'OK'
            AND r.baseline_anchor_at IS NOT NULL
            AND r.baseline_source IS NOT NULL
            AND NOT EXISTS (
                SELECT 1 FROM import_jobs j
                WHERE j.workspace_id = r.workspace_id AND j.kind = 'import'
                    AND j.state = 'finished' AND j.status = 'OK' AND j.finished_at IS NOT NULL
            )
) f
ORDER BY f.workspace_id, f.at;

-- A reply a person or an agent token wrote in Colliedesk that left it: not imported, not the
-- channel's own notice, queued by a delivery that is not failed (a reply saved with a failed
-- delivery, or with no outbox row, never left), and not to a conversation whose customer is one of
-- the workspace's agents (the answer to a test message). Its time is when it was queued: the outbox
-- row's creation, which a retry sets again.
INSERT INTO workspace_events(workspace_id, kind, at, source, detail)
SELECT DISTINCT ON (m.workspace_id) m.workspace_id, 'first_reply', q.at, 'backfill',
    jsonb_build_object('message_id', m.id, 'conversation_id', m.conversation_id)
FROM messages m
CROSS JOIN LATERAL (SELECT min(o.created_at) AS at FROM channel_outbox o
    WHERE o.workspace_id = m.workspace_id AND o.message_id = m.id AND o.status <> 'failed') q
WHERE m.kind = 'outgoing' AND m.origin IS NULL AND m.notice_kind IS NULL AND q.at IS NOT NULL
    AND (m.author_agent_id IS NOT NULL OR m.agent_token_id IS NOT NULL)
    AND NOT EXISTS (SELECT 1 FROM conversations c
        JOIN contacts k ON k.workspace_id = c.workspace_id AND k.id = c.contact_id
        JOIN agents a ON a.workspace_id = c.workspace_id AND a.email = lower(k.email)
        WHERE c.workspace_id = m.workspace_id AND c.id = m.conversation_id)
ORDER BY m.workspace_id, q.at, m.id;

-- An invite to someone who was not a member yet (an invite to a member names them in agent_id).
INSERT INTO workspace_events(workspace_id, kind, at, source, detail)
SELECT DISTINCT ON (workspace_id) workspace_id, 'teammate_invited', created_at, 'backfill',
    jsonb_build_object('invite_id', id)
FROM pairing_links
WHERE kind = 'invite' AND agent_id IS NULL AND created_at IS NOT NULL
ORDER BY workspace_id, created_at, id;

-- Billing, from the subscriptions as stored now and the webhooks that arrived, so a stale webhook
-- (one the server stored but did not apply: its subscription says otherwise) makes no event. The
-- server records every transition; the stored rows keep only the last state, so the backfill writes
-- at most one paid and one cancelled per workspace and a cancel followed by a resume is not
-- replayed. paid, per subscription: a subscription whose status is active now, or one with a
-- subscription.active webhook behind it. A webhook names no subscription, so it counts for a
-- workspace with exactly one stored subscription; with several, only the ones active now count.
-- past_due, unpaid, paused, trialing and incomplete alone are no payment, and a subscription still
-- incomplete never became active, whatever webhook arrived. The time is the first
-- subscription.active webhook of such a single-subscription workspace, else when the subscription
-- last changed (a reconcile stores an active one without a webhook). The earliest of a workspace's
-- subscriptions is its paid event.
INSERT INTO workspace_events(workspace_id, kind, at, source)
SELECT b.workspace_id, 'paid', min(b.at), 'backfill'
FROM (
    SELECT s.workspace_id, COALESCE(a.first_active, s.source_modified_at) AS at, s.status
    FROM workspace_billing s
    LEFT JOIN LATERAL (
        SELECT min(e.received_at) AS first_active FROM billing_webhook_events e
        WHERE e.workspace_id = s.workspace_id AND e.event_type = 'subscription.active'
            AND (SELECT count(*) FROM workspace_billing n WHERE n.workspace_id = s.workspace_id) = 1
    ) a ON true
    WHERE s.status = 'active'
        OR (a.first_active IS NOT NULL AND s.status NOT IN ('incomplete', 'incomplete_expired'))
) b
GROUP BY b.workspace_id
ORDER BY min(b.at), b.workspace_id;

-- cancelled: a subscription set to end at its period's end, or ended (status canceled, which a
-- revoke and the reconcile also set). Its time is the first cancel or revoke webhook, else when the
-- subscription ended or last changed; never before the workspace's paid event.
INSERT INTO workspace_events(workspace_id, kind, at, source)
SELECT c.workspace_id, 'cancelled',
    GREATEST(
        min(c.at),
        (SELECT min(p.at) FROM workspace_events p
            WHERE p.workspace_id = c.workspace_id AND p.kind = 'paid')
    ),
    'backfill'
FROM (
    SELECT s.workspace_id, COALESCE(
        (SELECT min(e.received_at) FROM billing_webhook_events e
            WHERE e.workspace_id = s.workspace_id
                AND e.event_type IN ('subscription.canceled', 'subscription.revoked')),
        s.ended_at, s.source_modified_at) AS at
    FROM workspace_billing s
    WHERE s.status = 'canceled' OR s.cancel_at_period_end
) c
GROUP BY c.workspace_id
ORDER BY min(c.at), c.workspace_id;
