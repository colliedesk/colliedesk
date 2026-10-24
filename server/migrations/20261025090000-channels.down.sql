-- The schema before channels has no place for what only channels can hold. Refuse to roll back while
-- any of it exists, rather than drop it silently.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM contacts WHERE email IS NULL) OR EXISTS (SELECT 1 FROM contact_identities) THEN
        RAISE EXCEPTION 'contacts known only on a channel exist; merge them into contacts with an email address or delete them before rolling back';
    END IF;
    IF EXISTS (SELECT 1 FROM channels WHERE type NOT IN ('email', 'web_form')) OR EXISTS (SELECT 1 FROM channel_outbox WHERE channel_type <> 'email') THEN
        RAISE EXCEPTION 'channels of a type other than email and web form exist; remove them before rolling back';
    END IF;
    IF EXISTS (SELECT 1 FROM channel_inbound_events WHERE state IN ('received', 'pending', 'held', 'failed')) THEN
        RAISE EXCEPTION 'channels hold messages that are not conversations yet; open or discard them before rolling back';
    END IF;
    -- The old schema cannot turn email off: rolling back would turn it on again without a word.
    IF EXISTS (SELECT 1 FROM channels WHERE type = 'email' AND deleted_at IS NULL AND NOT enabled) THEN
        RAISE EXCEPTION 'an email channel is off; turn it on or disconnect it before rolling back';
    END IF;
END
$$;

-- Lost on purpose, and nothing a person still needs: the names given to channels, their history
-- (channel_events), the events already processed (their content is in messages), removed channels
-- and channel secrets of types that cannot exist here. Everything else is a copy of old columns.
DROP TRIGGER conversations_email_thread_key ON conversations;
DROP FUNCTION conversations_email_thread_key();
DROP TRIGGER messages_email_external_id ON messages;
DROP FUNCTION messages_email_external_id();
DROP TRIGGER channel_outbox_default_channel ON channel_outbox;
DROP FUNCTION channel_outbox_default_channel();
DROP TRIGGER conversations_default_channel ON conversations;
DROP FUNCTION conversations_default_channel();
DROP TRIGGER web_form_channels_sync_enabled ON web_form_channels;
DROP FUNCTION web_form_channels_sync_enabled();
DROP TRIGGER web_form_channels_link_channel ON web_form_channels;
DROP FUNCTION web_form_channels_link_channel();
DROP TRIGGER email_channels_remove_channel ON email_channels;
DROP FUNCTION email_channels_remove_channel();
DROP TRIGGER email_channels_link_channel ON email_channels;
DROP FUNCTION email_channels_link_channel();

-- A web form that is off is off in web_form_channels too; channels.enabled was the truth.
UPDATE web_form_channels f SET enabled = c.enabled FROM channels c
    WHERE c.workspace_id = f.workspace_id AND c.id = f.channel_id AND f.enabled IS DISTINCT FROM c.enabled;

DROP VIEW email_outbox;
DROP INDEX channel_outbox_channel_failed_idx;
ALTER TABLE channel_outbox
    DROP CONSTRAINT channel_outbox_channel_fkey,
    DROP COLUMN meta,
    DROP COLUMN last_error_code,
    DROP COLUMN external_id,
    DROP COLUMN channel_id,
    DROP COLUMN channel_type;
UPDATE channel_outbox SET transport = 'smtp' WHERE transport IS NULL;
ALTER TABLE channel_outbox ALTER COLUMN transport SET NOT NULL;
ALTER TABLE channel_outbox RENAME TO email_outbox;

ALTER TABLE web_form_channels DROP CONSTRAINT web_form_channels_channel_fkey, DROP COLUMN channel_id;
ALTER TABLE email_channels DROP CONSTRAINT email_channels_channel_fkey, DROP COLUMN channel_id;
ALTER TABLE contacts ALTER COLUMN email SET NOT NULL;

DROP INDEX messages_channel_incoming_recent_idx;
DROP INDEX messages_workspace_external_id_idx;
ALTER TABLE messages
    DROP CONSTRAINT messages_channel_id_fkey,
    DROP COLUMN channel_meta,
    DROP COLUMN external_id,
    DROP COLUMN channel_id;

DROP INDEX conversations_workspace_channel_idx;
ALTER TABLE conversations
    DROP CONSTRAINT conversations_channel_id_fkey,
    DROP COLUMN channel_id,
    DROP CONSTRAINT conversations_channel_fkey,
    ADD CONSTRAINT conversations_channel_check CHECK (channel IN ('email', 'web_form', 'imported', 'agent', 'problem_report'));

DROP TABLE channel_events;
DROP TABLE channel_inbound_events;
DROP FUNCTION channel_inbound_events_count_held();
DROP TABLE conversation_thread_keys;
DROP TABLE contact_identities;
DROP TABLE channel_credentials;
DROP TABLE channels;
DROP TABLE channel_types;
