-- One row that names this install: a random id the daily usage report sends instead of anything
-- that identifies a person (docs/user/telemetry.md), and when it last reported, so every server on
-- the database together sends at most one report a day. It is created here so every install, new
-- or upgraded, has exactly one. Platform-level table: no workspace policy, only the privileged
-- telemetry transaction reads it. Older images ignore it.
CREATE TABLE instance (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    install_id uuid NOT NULL DEFAULT gen_random_uuid(),
    last_reported_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO instance DEFAULT VALUES;

ALTER TABLE instance ENABLE ROW LEVEL SECURITY;
ALTER TABLE instance FORCE ROW LEVEL SECURITY;
DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    -- The app role gets every new table by default; this one is for the privileged path only.
    EXECUTE format('REVOKE ALL ON instance FROM %I', app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON instance FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
END
$$;
