DROP TABLE IF EXISTS email_outbox;
DROP INDEX IF EXISTS messages_workspace_incoming_recent_idx;
DROP INDEX IF EXISTS messages_workspace_email_message_id_idx;
ALTER TABLE messages
    DROP CONSTRAINT IF EXISTS messages_workspace_id_id_key,
    DROP COLUMN IF EXISTS email_meta,
    DROP COLUMN IF EXISTS email_message_id;
ALTER TABLE conversations DROP COLUMN IF EXISTS email_reply_token;
DROP TABLE IF EXISTS email_channels;
