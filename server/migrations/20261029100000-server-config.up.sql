-- The server's configuration lives here, not in the environment (docs/internals/operator-console.md). Every
-- save is a version: a full snapshot of every key set, so restoring one is copying it into a new version and
-- nothing is ever lost. Secrets are sealed with MAILBOX_TOKEN_KEYS and bound to their key's name. None of these
-- tables has a workspace_id, and the app role gets no grant: only the privileged owner path reads them (same
-- shape as server_secrets).
CREATE TABLE server_config_versions (
    id bigserial PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now(),
    created_by text NOT NULL CHECK (char_length(created_by) BETWEEN 1 AND 200),
    source text NOT NULL CHECK (source IN ('env-import', 'console', 'cli', 'apply-file', 'restore', 'trial-auto-revert', 'request')),
    parent_id bigint NULL REFERENCES server_config_versions(id),
    note text NOT NULL DEFAULT '' CHECK (char_length(note) <= 500),
    status text NOT NULL DEFAULT 'saved' CHECK (status IN ('saved', 'trial', 'confirmed', 'reverted', 'rejected')),
    trial_expires_at timestamptz NULL,
    request_id uuid NULL,
    rejected_reason text NULL CHECK (char_length(rejected_reason) <= 2000)
);

CREATE TABLE server_config_values (
    version_id bigint NOT NULL REFERENCES server_config_versions(id) ON DELETE CASCADE,
    key text NOT NULL CHECK (key ~ '^[a-z][a-z0-9_]*(\.[a-z0-9_]+)*$'),
    value text NULL,
    sealed bytea NULL,
    seal_key_id text NULL,
    PRIMARY KEY (version_id, key),
    CHECK ((value IS NULL) <> (sealed IS NULL)),
    CHECK ((sealed IS NULL) = (seal_key_id IS NULL))
);

-- One row: the version every process should run, and the last one a process ran without an error.
CREATE TABLE server_config_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    desired_version bigint NULL REFERENCES server_config_versions(id),
    last_good_version bigint NULL REFERENCES server_config_versions(id),
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO server_config_state (singleton) VALUES (true);

-- Each running `collie serve` says here which version it runs; rows without a heartbeat for 10 minutes go.
CREATE TABLE server_processes (
    instance_id text PRIMARY KEY CHECK (char_length(instance_id) BETWEEN 1 AND 100),
    hostname text NOT NULL DEFAULT '',
    binary_version text NOT NULL DEFAULT '',
    running_version bigint NULL,
    started_at timestamptz NOT NULL DEFAULT now(),
    heartbeat_at timestamptz NOT NULL DEFAULT now(),
    last_error text NULL,
    waiting_for text NULL
);

-- The Polar products the server sells and recognizes. Add-only by design: a subscription's webhook names the
-- product it was bought as, so a product id must always map to the same plan. Only on_sale changes.
CREATE TABLE polar_products (
    product_id text PRIMARY KEY CHECK (char_length(product_id) BETWEEN 1 AND 200),
    plan text NOT NULL CHECK (plan IN ('team', 'founding', 'solo', 'small', 'team25')),
    -- single: a plan sold as one product whatever its billing period (Founding).
    period text NOT NULL CHECK (period IN ('monthly', 'yearly', 'single')),
    on_sale boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    created_by text NOT NULL CHECK (char_length(created_by) BETWEEN 1 AND 200)
);
CREATE UNIQUE INDEX polar_products_one_on_sale ON polar_products (plan, period) WHERE on_sale;

CREATE FUNCTION polar_products_add_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'polar_products is add-only: take a product off sale instead of deleting it';
    END IF;
    IF NEW.product_id <> OLD.product_id OR NEW.plan <> OLD.plan OR NEW.period <> OLD.period
        OR NEW.created_at <> OLD.created_at OR NEW.created_by <> OLD.created_by THEN
        RAISE EXCEPTION 'polar_products is add-only: only on_sale changes';
    END IF;
    RETURN NEW;
END
$$;
CREATE TRIGGER polar_products_add_only BEFORE UPDATE OR DELETE ON polar_products
    FOR EACH ROW EXECUTE FUNCTION polar_products_add_only();

-- What changed the server's configuration and its operators, and who did it. Append-only; never a secret's value.
CREATE TABLE operator_audit (
    id bigserial PRIMARY KEY,
    at timestamptz NOT NULL DEFAULT now(),
    actor_kind text NOT NULL CHECK (actor_kind IN ('operator', 'cli', 'server', 'token')),
    actor text NOT NULL CHECK (char_length(actor) BETWEEN 1 AND 200),
    action text NOT NULL CHECK (char_length(action) BETWEEN 1 AND 100),
    version_id bigint NULL REFERENCES server_config_versions(id),
    keys text[] NOT NULL DEFAULT '{}',
    detail jsonb NOT NULL DEFAULT '{}',
    ip inet NULL
);
CREATE INDEX operator_audit_at ON operator_audit (at DESC);

CREATE FUNCTION operator_audit_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'operator_audit is append-only';
END
$$;
CREATE TRIGGER operator_audit_append_only BEFORE UPDATE OR DELETE ON operator_audit
    FOR EACH ROW EXECUTE FUNCTION operator_audit_append_only();

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
    FOREACH name IN ARRAY ARRAY['server_config_versions', 'server_config_values', 'server_config_state',
        'server_processes', 'polar_products', 'operator_audit'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', name);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', name);
        -- The app role gets every new table by default; these are for the privileged path only.
        EXECUTE format('REVOKE ALL ON %I FROM %I', name, app_role);
        EXECUTE format($policy$CREATE POLICY privileged_access ON %1$I FOR ALL TO %2$I
            USING ((SELECT current_setting('app.privileged', true) = 'on'))
            WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, name, current_user);
    END LOOP;
    EXECUTE format('REVOKE ALL ON SEQUENCE server_config_versions_id_seq, operator_audit_id_seq FROM %I', app_role);
END
$$;
