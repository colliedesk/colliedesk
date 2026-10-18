-- A complaint about mail sent as a verified support address names no reply we can find (the relay
-- rewrites the Message-ID), but it still counts toward the workspace's complaint rate, so an event may
-- have no message.
ALTER TABLE relay_webhook_events ALTER COLUMN message_id DROP NOT NULL;

-- When an operator lifts a pause, complaints and sends before that moment stop counting toward it.
ALTER TABLE workspaces ADD COLUMN email_pause_resumed_at timestamptz;

-- Marks a queued reply that waits only because its workspace's sending is paused, so lifting the pause
-- releases those and leaves replies in error backoff alone.
ALTER TABLE email_outbox ADD COLUMN held_by_pause boolean NOT NULL DEFAULT false;
