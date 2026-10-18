-- Code older than this migration does not know the pause and would send again. Queued replies stay
-- queued, and the relay's own complaint handling still applies.
ALTER TABLE workspaces
    DROP CONSTRAINT IF EXISTS workspaces_email_pause_complete,
    DROP COLUMN IF EXISTS email_pause_reason,
    DROP COLUMN IF EXISTS email_paused_at;
