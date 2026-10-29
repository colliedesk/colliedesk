-- The people who run the whole server (the operator console), apart from every workspace's agents: their
-- one-time sign-in links, sessions and tokens, and the changes their tokens ask a person to approve
-- (docs/internals/operator-console.md). No workspace_id, no grant to the app role: the privileged owner
-- path only, as server_secrets.
CREATE TABLE operators (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email text NOT NULL CHECK (email = lower(email) AND char_length(email) BETWEEN 3 AND 320 AND position('@' IN email) > 1),
    created_at timestamptz NOT NULL DEFAULT now(),
    created_by text NOT NULL CHECK (char_length(created_by) BETWEEN 1 AND 200),
    revoked_at timestamptz NULL,
    totp_sealed bytea NULL,
    totp_key_id text NULL,
    totp_enabled_at timestamptz NULL,
    totp_last_step bigint NOT NULL DEFAULT 0,
    failed_codes int NOT NULL DEFAULT 0,
    locked_until timestamptz NULL,
    CHECK ((totp_sealed IS NULL) = (totp_key_id IS NULL)),
    CHECK ((totp_enabled_at IS NULL) OR (totp_sealed IS NOT NULL))
);
CREATE UNIQUE INDEX operators_email ON operators (email);

-- A link works once, for 15 minutes; only its hash is stored. A sign-in link opens a ticket (also a link
-- row) for the authenticator step, which works once for 10 minutes.
CREATE TABLE operator_links (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    operator_id uuid NOT NULL REFERENCES operators(id),
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    purpose text NOT NULL CHECK (purpose IN ('sign-in', 'reset-totp', 'totp-ticket')),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at timestamptz NULL,
    created_by text NOT NULL CHECK (char_length(created_by) BETWEEN 1 AND 200),
    -- A ticket remembers the authenticator key it enrolls, sealed, until the code proves it.
    enroll_sealed bytea NULL,
    enroll_key_id text NULL
);
CREATE INDEX operator_links_operator ON operator_links (operator_id);

CREATE TABLE operator_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    operator_id uuid NOT NULL REFERENCES operators(id),
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    kind text NOT NULL CHECK (kind IN ('cookie', 'token')),
    label text NOT NULL DEFAULT '' CHECK (char_length(label) <= 100),
    permissions text[] NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    reauth_at timestamptz NULL,
    revoked_at timestamptz NULL,
    created_by_session uuid NULL REFERENCES operator_sessions(id)
);
CREATE INDEX operator_sessions_operator ON operator_sessions (operator_id);

-- A change an operator token asked for, waiting for a person (an operator) to approve it. Its body never
-- holds a secret: the approver types any secret the change needs, and it is used once, never stored.
CREATE TABLE operator_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id uuid NOT NULL REFERENCES operator_sessions(id),
    method text NOT NULL CHECK (method IN ('POST', 'PATCH', 'PUT', 'DELETE')),
    route text NOT NULL CHECK (char_length(route) BETWEEN 1 AND 300),
    path text NOT NULL CHECK (char_length(path) BETWEEN 1 AND 500),
    body jsonb NULL,
    summary text NOT NULL CHECK (char_length(summary) BETWEEN 1 AND 500),
    note text NOT NULL DEFAULT '' CHECK (char_length(note) <= 500),
    needs_secrets text[] NOT NULL DEFAULT '{}',
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'expired', 'failed', 'cancelled')),
    decided_by uuid NULL REFERENCES operators(id),
    decided_at timestamptz NULL,
    result_status int NULL,
    result jsonb NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);
CREATE INDEX operator_requests_pending ON operator_requests (created_at) WHERE status = 'pending';

-- What an operator's POST answered for its Idempotency-Key, for a day: a repeated press answers the first
-- result (without anything shown once) instead of doing it twice.
CREATE TABLE operator_idempotency (
    operator_id uuid NOT NULL REFERENCES operators(id),
    operation text NOT NULL CHECK (char_length(operation) BETWEEN 1 AND 100),
    key text NOT NULL CHECK (char_length(key) BETWEEN 1 AND 200),
    fingerprint bytea NOT NULL,
    answer jsonb NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (operator_id, operation, key)
);

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
    FOREACH name IN ARRAY ARRAY['operators', 'operator_links', 'operator_sessions', 'operator_requests', 'operator_idempotency'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', name);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', name);
        EXECUTE format('REVOKE ALL ON %I FROM %I', name, app_role);
        EXECUTE format($policy$CREATE POLICY privileged_access ON %1$I FOR ALL TO %2$I
            USING ((SELECT current_setting('app.privileged', true) = 'on'))
            WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, name, current_user);
    END LOOP;
END
$$;
