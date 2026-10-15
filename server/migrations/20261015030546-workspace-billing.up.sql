-- One row per Polar subscription of a workspace; a workspace with no row is on the free plan. Each
-- row mirrors the latest event of its subscription. A workspace can briefly hold two subscriptions
-- (two checkouts), and the plan in effect is the best one that is still paid, so cancelling a
-- spare never takes the plan from the one that still charges.
CREATE TABLE workspace_billing (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    polar_subscription_id text NOT NULL,
    plan text NOT NULL CHECK (plan IN ('team', 'founding')),
    polar_customer_id text NOT NULL,
    -- A status Polar adds later is stored as 'unknown' (not paid) instead of failing the webhook.
    status text NOT NULL CHECK (status IN ('incomplete', 'incomplete_expired', 'trialing', 'active', 'past_due', 'canceled', 'unpaid', 'paused', 'unknown')),
    current_period_end timestamptz,
    cancel_at_period_end boolean NOT NULL DEFAULT false,
    ended_at timestamptz,
    -- The subscription's own modified_at; an event older than this one is out of order and ignored.
    source_modified_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, polar_subscription_id)
);

-- Webhook ids already processed, so a redelivery never writes twice. Scoped by workspace so one
-- workspace's events never decide another's.
CREATE TABLE billing_webhook_events (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    webhook_id text NOT NULL,
    event_type text NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, webhook_id)
);

-- Same isolation as the tables of the row-level security migration; the app role comes from the
-- policy on workspaces, so this migration needs no role name of its own.
DO $$
DECLARE
    app_role text;
    table_name text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    FOREACH table_name IN ARRAY ARRAY['workspace_billing', 'billing_webhook_events']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', table_name);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', table_name);
        EXECUTE format($policy$CREATE POLICY workspace_isolation ON %1$I FOR ALL TO %2$I
            USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
            WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$,
            table_name, app_role);
        EXECUTE format($policy$CREATE POLICY privileged_access ON %1$I FOR ALL TO %2$I
            USING ((SELECT current_setting('app.privileged', true) = 'on'))
            WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$,
            table_name, current_user);
        -- Tables made by a role other than the one that ran the row-level security migration do not
        -- inherit its default privileges, so grant them here.
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON %I TO %I', table_name, app_role);
    END LOOP;
END
$$;
