-- The Colliedesk Cloud reports to Meta's Conversions API that a sign-up from a Meta ad started a trial
-- (StartTrial) and, later, a paid plan (Subscribe). One row per workspace and event, written when the
-- event happens and sent by a worker, so a failed call is retried with the same event and never delays
-- the sign-up or the checkout. What is sent is only what the privacy page lists: the click (fbc), the
-- sign-up's client address and browser, the workspace id as the de-duplication key, and the plan's price.
-- client_ip and user_agent are cleared once nothing more can be sent with them. Older images ignore this table.
CREATE TABLE meta_conversion_events (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    event_name text NOT NULL CHECK (event_name IN ('StartTrial', 'Subscribe')),
    event_time timestamptz NOT NULL,
    fbc text NOT NULL CHECK (fbc ~ '^fb\.1\.[0-9]+\.[A-Za-z0-9._-]+$' AND length(fbc) <= 340),
    client_ip text,
    user_agent text CHECK (length(user_agent) <= 1024),
    event_source_url text NOT NULL,
    value numeric(12, 2) CHECK (value >= 0),
    currency text CHECK (currency ~ '^[A-Z]{3}$'),
    attempts integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL,
    sent_at timestamptz,
    abandoned_at timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, event_name),
    CHECK ((value IS NULL) = (currency IS NULL))
);
CREATE INDEX meta_conversion_events_due ON meta_conversion_events (next_attempt_at)
    WHERE sent_at IS NULL AND abandoned_at IS NULL;

-- Same isolation as workspace_onboarding.
DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE meta_conversion_events ENABLE ROW LEVEL SECURITY;
    ALTER TABLE meta_conversion_events FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON meta_conversion_events FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON meta_conversion_events FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON meta_conversion_events TO %I', app_role);
END
$$;
