-- The schema before channels has room for one email channel and one web form per workspace and for
-- nothing only channels can hold. Refuse to roll back while any of that exists, rather than drop it
-- silently.
DO $$
DECLARE
    many text;
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
    -- The old schema has one email channel and one web form per workspace.
    SELECT string_agg(format('%s: %s', w.slug, names), '; ' ORDER BY w.slug) INTO many FROM (
        SELECT e.workspace_id, string_agg(format('%s <%s>', c.name, e.support_address), ', ' ORDER BY c.created_at, c.id) AS names
        FROM email_channels e JOIN channels c ON c.workspace_id = e.workspace_id AND c.id = e.channel_id
        GROUP BY e.workspace_id HAVING count(*) > 1) two JOIN workspaces w ON w.id = two.workspace_id;
    IF many IS NOT NULL THEN
        RAISE EXCEPTION 'a workspace has more than one email channel (%); remove all but one before rolling back', many;
    END IF;
    SELECT string_agg(format('%s: %s', w.slug, names), '; ' ORDER BY w.slug) INTO many FROM (
        SELECT f.workspace_id, string_agg(c.name, ', ' ORDER BY c.created_at, c.id) AS names
        FROM web_form_channels f JOIN channels c ON c.workspace_id = f.workspace_id AND c.id = f.channel_id
        GROUP BY f.workspace_id HAVING count(*) > 1) two JOIN workspaces w ON w.id = two.workspace_id;
    IF many IS NOT NULL THEN
        RAISE EXCEPTION 'a workspace has more than one web form (%); remove all but one before rolling back', many;
    END IF;
    -- The old schema cannot turn email off: rolling back would turn it on again without a word.
    IF EXISTS (SELECT 1 FROM channels WHERE type = 'email' AND deleted_at IS NULL AND NOT enabled) THEN
        RAISE EXCEPTION 'an email channel is off; turn it on or disconnect it before rolling back';
    END IF;
END
$$;

-- Lost on purpose, and nothing a person still needs: the names given to channels and which was the
-- default, their history (channel_events), the events already processed (their content is in
-- messages), removed channels, the channel a conversation's replies were pinned to (each type has one
-- channel at most now), forwarding local parts (the old schema routes every local part of the hosted
-- subdomain to the one channel) and channel secrets of types that cannot exist here.

-- Message-IDs and reply tokens go back to their columns.
DROP INDEX conversation_thread_keys_email_token_idx;
ALTER TABLE conversations ADD COLUMN email_reply_token text UNIQUE;
UPDATE conversations v SET email_reply_token = k.token FROM (
        SELECT DISTINCT ON (workspace_id, conversation_id) workspace_id, conversation_id, substr(key, 13) AS token
        FROM conversation_thread_keys WHERE channel_type = 'email' AND key LIKE 'reply-token:%'
        ORDER BY workspace_id, conversation_id, created_at) k
    WHERE k.workspace_id = v.workspace_id AND k.conversation_id = v.id;
ALTER TABLE messages ADD COLUMN email_message_id text;
UPDATE messages SET email_message_id = substr(external_id, 7) WHERE external_id LIKE 'email:%';
CREATE UNIQUE INDEX messages_workspace_email_message_id_idx ON messages(workspace_id, email_message_id) WHERE email_message_id IS NOT NULL;

-- The outbox under its old name.
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

-- A web form is on or off in its own row again, keyed by its workspace.
DROP INDEX web_form_channels_legacy_slug_idx;
ALTER TABLE web_form_channels ADD COLUMN enabled boolean NOT NULL DEFAULT false;
UPDATE web_form_channels f SET enabled = c.enabled FROM channels c WHERE c.workspace_id = f.workspace_id AND c.id = f.channel_id;
ALTER TABLE web_form_channels DROP CONSTRAINT web_form_channels_pkey, ADD PRIMARY KEY (workspace_id),
    DROP CONSTRAINT web_form_channels_channel_fkey, DROP COLUMN legacy_slug, DROP COLUMN channel_id;

-- The domain state goes back to the one email channel of each workspace. A provider domain no channel
-- uses any more is queued for deletion at the provider, as removing its channel would have done.
ALTER TABLE email_channels
    ADD COLUMN domain_verification_token text,
    ADD COLUMN domain_verified_at timestamptz,
    ADD COLUMN provider_domain_id text,
    ADD COLUMN provider_domain_records jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN provider_domain_created_at timestamptz,
    ADD COLUMN provider_domain_expired_at timestamptz,
    ADD COLUMN provider_domain_first_verified_at timestamptz;
UPDATE email_channels e SET domain_verification_token = d.verification_token, domain_verified_at = d.verified_at,
        provider_domain_id = d.provider_domain_id, provider_domain_records = d.provider_domain_records,
        provider_domain_created_at = d.provider_domain_created_at, provider_domain_expired_at = d.provider_domain_expired_at,
        provider_domain_first_verified_at = d.provider_domain_first_verified_at
    FROM email_domains d WHERE d.workspace_id = e.workspace_id AND d.domain = e.domain;
INSERT INTO email_provider_domain_trash (workspace_id, provider_domain_id)
    SELECT workspace_id, provider_domain_id FROM email_domains d
    WHERE provider_domain_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM email_channels e WHERE e.workspace_id = d.workspace_id AND e.domain = d.domain)
    ON CONFLICT DO NOTHING;
ALTER TABLE email_channels ALTER COLUMN domain_verification_token SET NOT NULL;

-- Gmail connections and consents point at their workspace's email channel again.
ALTER TABLE mailbox_oauth_states DROP CONSTRAINT mailbox_oauth_states_channel_fkey, DROP COLUMN channel_id;
DROP INDEX mailbox_connections_workspace_address_idx;
ALTER TABLE mailbox_connections DROP CONSTRAINT mailbox_connections_channel_fkey, DROP CONSTRAINT mailbox_connections_workspace_id_fkey,
    DROP CONSTRAINT mailbox_connections_pkey;
DROP INDEX email_channels_hosted_local_idx;
DROP INDEX email_channels_mailbox_idx;
ALTER TABLE email_channels DROP CONSTRAINT email_channels_domain_fkey, DROP CONSTRAINT email_channels_workspace_channel_key,
    DROP CONSTRAINT email_channels_pkey, ADD PRIMARY KEY (workspace_id),
    DROP CONSTRAINT email_channels_channel_fkey, DROP CONSTRAINT email_channels_support_address_shape,
    DROP COLUMN mailbox_key, DROP COLUMN domain, DROP COLUMN hosted_local, DROP COLUMN channel_id;
CREATE UNIQUE INDEX email_channels_provider_domain_id_key ON email_channels (provider_domain_id) WHERE provider_domain_id IS NOT NULL;
CREATE UNIQUE INDEX email_channels_provider_verified_domain_key ON email_channels ((split_part(support_address, '@', 2)))
    WHERE provider_domain_id IS NOT NULL AND domain_verified_at IS NOT NULL;
ALTER TABLE mailbox_connections DROP COLUMN channel_id, ADD PRIMARY KEY (workspace_id),
    ADD CONSTRAINT mailbox_connections_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES email_channels(workspace_id) ON DELETE CASCADE;
DROP TABLE email_domains;

ALTER TABLE contacts ALTER COLUMN email SET NOT NULL;

DROP INDEX messages_channel_incoming_recent_idx;
DROP INDEX messages_workspace_external_id_idx;
ALTER TABLE messages
    DROP CONSTRAINT messages_channel_id_fkey,
    DROP COLUMN channel_meta,
    DROP COLUMN external_id,
    DROP COLUMN channel_id;

DROP INDEX conversations_workspace_reply_channel_idx;
DROP INDEX conversations_workspace_channel_idx;
ALTER TABLE conversations
    DROP CONSTRAINT conversations_reply_channel_fkey,
    DROP CONSTRAINT conversations_channel_id_fkey,
    DROP COLUMN reply_channel_id,
    DROP COLUMN channel_id,
    DROP CONSTRAINT conversations_channel_fkey,
    ADD CONSTRAINT conversations_channel_check CHECK (channel IN ('email', 'web_form', 'imported', 'agent', 'problem_report'));

DROP TABLE channel_events;
DROP TABLE channel_sent_ids;
DROP TABLE channel_inbound_files;
DROP FUNCTION channel_inbound_files_count();
DROP TABLE channel_inbound_events;
DROP FUNCTION channel_inbound_events_count_held();
DROP TABLE conversation_thread_keys;
DROP TABLE contact_identities;
DROP TABLE channel_credentials;
DROP TABLE channels;
DROP TABLE channel_types;
