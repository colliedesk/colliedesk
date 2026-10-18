-- Events without a message cannot be stored by older code, so they are deleted; they only fed the
-- complaint count, and older code does not read it.
DELETE FROM relay_webhook_events WHERE message_id IS NULL;
ALTER TABLE relay_webhook_events ALTER COLUMN message_id SET NOT NULL;
ALTER TABLE workspaces DROP COLUMN IF EXISTS email_pause_resumed_at;
ALTER TABLE email_outbox DROP COLUMN IF EXISTS held_by_pause;
