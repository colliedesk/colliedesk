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

-- The app role gets every new table by default; this one is for the privileged path only.
SELECT secure_server_table('instance');
