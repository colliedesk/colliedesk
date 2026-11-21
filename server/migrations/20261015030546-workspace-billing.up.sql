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
    status text NOT NULL CHECK (status IN (
        'incomplete', 'incomplete_expired', 'trialing', 'active', 'past_due', 'canceled', 'unpaid',
        'paused', 'unknown'
    )),
    current_period_end timestamptz,
    cancel_at_period_end boolean NOT NULL DEFAULT false,
    ended_at timestamptz,
    -- The subscription's own modified_at; an event older than this one is out of order
    -- and ignored.
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

-- Same isolation as the tables of the row-level security migration.
SELECT secure_workspace_table('workspace_billing');
SELECT secure_workspace_table('billing_webhook_events');
