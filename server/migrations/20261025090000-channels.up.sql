-- Channels (docs/internals/channels.md). A channel is one way customers reach a workspace (its email
-- address, its web form, later a chat account): one row in channels plus the adapter of its type.
-- Everything that arrives goes through one ingest pipeline and every reply leaves through one outbox,
-- whatever the type. Email and the web form keep their own settings tables, keyed by their channel: a
-- workspace may have several channels of a type, one of them its default.
--
-- An image older than this migration does not run on this schema: the old columns are moved, not
-- kept twice. Rolling back is restoring the backup taken before the upgrade with the older image
-- (docs/self-host/upgrade.md); the down migration exists for a workspace that still fits the old
-- schema, and refuses otherwise.

-- The registered channel types. connectable=false is only an origin of conversations (an import, a
-- conversation an agent started); nothing connects to it. A workspace may have any number of channels
-- of a type (two support mailboxes, two web forms). A new type adds its row in its own migration. Not
-- workspace data: no workspace_id and no grant to the app role (revoked below), which never reads it: the server's
-- registry knows the types, and foreign keys check it as the owner.
CREATE TABLE channel_types (
    type text PRIMARY KEY CHECK (type ~ '^[a-z][a-z0-9_]{1,30}$'),
    connectable boolean NOT NULL,
    UNIQUE (type, connectable)
);
INSERT INTO channel_types (type, connectable) VALUES
    ('email', true), ('web_form', true), ('imported', false), ('agent', false), ('problem_report', false);

CREATE TABLE channels (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    type text NOT NULL,
    -- Checked against channel_types through the foreign key below, so only a connectable type has channels.
    connectable boolean NOT NULL DEFAULT true CHECK (connectable),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80 AND btrim(name) <> '' AND name !~ '[\x01-\x1f\x7f]'),
    enabled boolean NOT NULL DEFAULT true,
    -- The channel's rules for what arrives and what leaves (join policy, default tags, assignee and
    -- priority, rate limits, aggregation, customer notices), validated by its adapter. Never a secret.
    settings jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(settings) = 'object' AND octet_length(settings::text) <= 65536),
    -- The account outside that the channel is (a chat account id, a page id). Unique on the whole
    -- server: one account belongs to one workspace, so a shared webhook can route by it.
    external_account text CHECK (char_length(external_account) BETWEEN 1 AND 200),
    created_by_agent_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    disabled_at timestamptz,
    disabled_by_agent_id uuid,
    -- Removing a channel keeps its row, so conversations still name it; its settings and secrets go.
    deleted_at timestamptz,
    deleted_by_agent_id uuid,
    -- The channel of its type that answers what names no channel: a conversation started without
    -- choosing one, the routes of one email channel and one web form, a hosted address no channel
    -- claims. At most one per type, and a removed channel never is it.
    is_default boolean NOT NULL DEFAULT false,
    UNIQUE (workspace_id, id),
    FOREIGN KEY (type, connectable) REFERENCES channel_types(type, connectable),
    FOREIGN KEY (workspace_id, created_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (created_by_agent_id),
    FOREIGN KEY (workspace_id, disabled_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (disabled_by_agent_id),
    FOREIGN KEY (workspace_id, deleted_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (deleted_by_agent_id),
    CHECK (enabled = (disabled_at IS NULL)),
    CHECK (deleted_at IS NULL OR NOT enabled),
    CHECK (NOT is_default OR deleted_at IS NULL)
);
CREATE UNIQUE INDEX channels_default_idx ON channels(workspace_id, type) WHERE is_default;
-- Two live channels of a type never share a name, so a person can tell them apart.
CREATE UNIQUE INDEX channels_name_idx ON channels(workspace_id, type, lower(name)) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX channels_external_account_idx ON channels(type, external_account) WHERE external_account IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX channels_workspace_idx ON channels(workspace_id, created_at) WHERE deleted_at IS NULL;

-- A channel's secrets, sealed with tokenseal (MAILBOX_TOKEN_KEYS) under the context
-- channel:<workspace>:<channel>:<name>. version grows with every change, so a token that may be used
-- once (a refresh token) is replaced with a compare-and-swap.
CREATE TABLE channel_credentials (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id uuid NOT NULL,
    name text NOT NULL CHECK (name ~ '^[a-z][a-z0-9_]{0,40}$'),
    key_id text NOT NULL,
    sealed bytea NOT NULL,
    -- The last four characters, the only part ever shown again.
    hint text CHECK (char_length(hint) BETWEEN 1 AND 4),
    expires_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
    updated_by_agent_id uuid,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, channel_id, name),
    FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, updated_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (updated_by_agent_id)
);

-- Who a contact is on one channel (a chat user id). An email address stays in contacts.email.
CREATE TABLE contact_identities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    contact_id uuid NOT NULL,
    channel_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind ~ '^[a-z][a-z0-9_]{1,40}$'),
    value text NOT NULL CHECK (char_length(value) BETWEEN 1 AND 200),
    display_name text CHECK (char_length(display_name) <= 200),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, channel_id, kind, value),
    FOREIGN KEY (workspace_id, contact_id) REFERENCES contacts(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id)
);
CREATE INDEX contact_identities_contact_idx ON contact_identities(workspace_id, contact_id);

-- What joins a new message to a conversation, per channel type: an email reply token, a chat user or
-- group. One key of a chat may have had several conversations over time; the newest is tried first.
CREATE TABLE conversation_thread_keys (
    workspace_id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    channel_type text NOT NULL REFERENCES channel_types(type),
    key text NOT NULL CHECK (char_length(key) BETWEEN 1 AND 500),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, channel_type, key, conversation_id),
    FOREIGN KEY (workspace_id, conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE CASCADE
);
CREATE INDEX conversation_thread_keys_latest_idx ON conversation_thread_keys(workspace_id, channel_type, key, created_at DESC);

-- Every raw event a channel receives, written and committed before it is processed, so nothing is
-- lost when processing fails and an event can be processed again.
--   received   waiting to be processed      pending    collected with others until flush_at
--   held       kept for a person (hold_reason: channel_off when the channel was off, review)
--   processed  done (conversation_id and message_id, or none for an archived event)
--   dropped    not a message for the team (drop_reason)
--   failed     processing failed (error); the worker tries again
--   discarded  an admin threw a held event away (recorded in channel_events)
CREATE TABLE channel_inbound_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id uuid NOT NULL,
    external_event_id text CHECK (char_length(external_event_id) BETWEEN 1 AND 998),
    state text NOT NULL CHECK (state IN ('received', 'pending', 'held', 'processed', 'dropped', 'failed', 'discarded')),
    -- A small payload (a webhook body, a form post) up to 256 KiB, or the storage key of a large one
    -- (an email's raw message, already stored once as its original).
    raw jsonb,
    raw_object_key text,
    -- The size of the object at raw_object_key. A held event's bytes count toward the workspace's
    -- storage until the event becomes a message (whose attachment then counts them) or is discarded.
    raw_bytes bigint CHECK (raw_bytes >= 0),
    -- Who sent it on the channel (an identity), for the per-sender rate limit.
    sender_key text CHECK (char_length(sender_key) BETWEEN 1 AND 500),
    hold_reason text CHECK (hold_reason ~ '^[a-z][a-z_]{1,30}$'),
    aggregate_key text CHECK (char_length(aggregate_key) BETWEEN 1 AND 500),
    flush_at timestamptz,
    drop_reason text,
    error text,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    conversation_id uuid,
    message_id uuid,
    decided_by_agent_id uuid,
    received_at timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz,
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id),
    FOREIGN KEY (workspace_id, conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE SET NULL (conversation_id),
    FOREIGN KEY (workspace_id, message_id) REFERENCES messages(workspace_id, id) ON DELETE SET NULL (message_id),
    FOREIGN KEY (workspace_id, decided_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (decided_by_agent_id),
    CHECK (raw IS NOT NULL OR raw_object_key IS NOT NULL),
    CHECK (raw_bytes IS NULL OR raw_object_key IS NOT NULL),
    CHECK (raw IS NULL OR octet_length(raw::text) <= 262144),
    CHECK ((state = 'pending') = (flush_at IS NOT NULL)),
    CHECK ((state = 'dropped') = (drop_reason IS NOT NULL)),
    CHECK ((state = 'held') = (hold_reason IS NOT NULL))
);
CREATE UNIQUE INDEX channel_inbound_events_dedupe_idx ON channel_inbound_events(workspace_id, channel_id, external_event_id) WHERE external_event_id IS NOT NULL;
CREATE INDEX channel_inbound_events_channel_idx ON channel_inbound_events(workspace_id, channel_id, received_at DESC);
CREATE INDEX channel_inbound_events_due_idx ON channel_inbound_events(flush_at) WHERE state = 'pending';
CREATE INDEX channel_inbound_events_retry_idx ON channel_inbound_events(received_at) WHERE state IN ('received', 'failed');
CREATE INDEX channel_inbound_events_held_idx ON channel_inbound_events(workspace_id, channel_id, hold_reason) WHERE state = 'held';
-- An object a waiting event points at is not deleted with the attachment that shared it.
CREATE INDEX channel_inbound_events_object_idx ON channel_inbound_events(workspace_id, raw_object_key)
    WHERE raw_object_key IS NOT NULL AND state IN ('received', 'pending', 'held', 'failed');
CREATE INDEX channel_inbound_events_sender_idx ON channel_inbound_events(workspace_id, channel_id, sender_key, received_at)
    WHERE sender_key IS NOT NULL;

-- Held raw bytes count toward storage like attachments (20261015091216-founding-storage-counter), so a
-- channel left off cannot be filled without limit. Row-level: held events are few.
CREATE FUNCTION channel_inbound_events_count_held() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    delta bigint := 0;
    workspace uuid;
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') AND OLD.state = 'held' THEN
        delta := delta - COALESCE(OLD.raw_bytes, 0);
        workspace := OLD.workspace_id;
    END IF;
    IF TG_OP IN ('INSERT', 'UPDATE') AND NEW.state = 'held' THEN
        delta := delta + COALESCE(NEW.raw_bytes, 0);
        workspace := NEW.workspace_id;
    END IF;
    IF delta <> 0 THEN
        UPDATE workspaces SET counted_storage_bytes = counted_storage_bytes + delta, stored_storage_bytes = stored_storage_bytes + delta
            WHERE id = workspace;
    END IF;
    RETURN NULL;
END
$$;
CREATE TRIGGER channel_inbound_events_count_held AFTER INSERT OR UPDATE OF state, raw_bytes OR DELETE ON channel_inbound_events
    FOR EACH ROW EXECUTE FUNCTION channel_inbound_events_count_held();

-- The files of a journal event, fetched into attachment storage right after the event was journaled
-- and before it is parsed, held or collected: a channel's media links expire, and a message opened
-- days later must still have its files. A file that could not be fetched stays as failed with why.
-- Stored bytes count toward the workspace's storage until the file becomes an attachment.
CREATE TABLE channel_inbound_files (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    event_id uuid NOT NULL,
    idx integer NOT NULL CHECK (idx >= 0),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 255),
    content_type text NOT NULL CHECK (char_length(content_type) BETWEEN 1 AND 255),
    status text NOT NULL CHECK (status IN ('pending', 'stored', 'failed')),
    storage_key text,
    sha256 text CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    size bigint CHECK (size >= 0),
    error text CHECK (char_length(error) <= 500),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    -- Set once the file is an attachment of the message, which counts it from then on.
    attached boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, event_id, idx),
    FOREIGN KEY (workspace_id, event_id) REFERENCES channel_inbound_events(workspace_id, id) ON DELETE CASCADE,
    CHECK ((status = 'stored') = (storage_key IS NOT NULL AND sha256 IS NOT NULL AND size IS NOT NULL)),
    CHECK ((status = 'failed') = (error IS NOT NULL)),
    CHECK (NOT attached OR status = 'stored')
);
CREATE INDEX channel_inbound_files_object_idx ON channel_inbound_files(workspace_id, storage_key) WHERE status = 'stored' AND NOT attached;

CREATE FUNCTION channel_inbound_files_count() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    delta bigint := 0;
    workspace uuid;
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') AND OLD.status = 'stored' AND NOT OLD.attached THEN
        delta := delta - OLD.size;
        workspace := OLD.workspace_id;
    END IF;
    IF TG_OP IN ('INSERT', 'UPDATE') AND NEW.status = 'stored' AND NOT NEW.attached THEN
        delta := delta + NEW.size;
        workspace := NEW.workspace_id;
    END IF;
    IF delta <> 0 THEN
        UPDATE workspaces SET counted_storage_bytes = counted_storage_bytes + delta, stored_storage_bytes = stored_storage_bytes + delta
            WHERE id = workspace;
    END IF;
    RETURN NULL;
END
$$;
CREATE TRIGGER channel_inbound_files_count AFTER INSERT OR UPDATE OF status, size, attached OR DELETE ON channel_inbound_files
    FOR EACH ROW EXECUTE FUNCTION channel_inbound_files_count();

-- Every id a channel gave a reply it sent (a long reply goes out as several messages), so the copy
-- the channel sends back is known as our own.
CREATE TABLE channel_sent_ids (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id uuid NOT NULL,
    external_id text NOT NULL CHECK (char_length(external_id) BETWEEN 1 AND 1000),
    message_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, channel_id, external_id),
    FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id),
    FOREIGN KEY (workspace_id, message_id) REFERENCES messages(workspace_id, id) ON DELETE CASCADE
);

-- Who added, renamed, turned on or off, changed or removed a channel, and what happened to the
-- messages it held. Never a secret: only its last four characters. The name and type are copied.
CREATE TABLE channel_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Orders the events of one transaction, which share created_at (as ai_settings_events.seq).
    seq bigint GENERATED BY DEFAULT AS IDENTITY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id uuid NOT NULL,
    channel_type text NOT NULL,
    channel_name text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('added', 'renamed', 'enabled', 'disabled', 'config_changed', 'credential_changed',
        'removed', 'held_replayed', 'held_discarded', 'default_changed')),
    actor_agent_id uuid,
    -- The agent token's session when a token made the change.
    actor_token_id uuid,
    detail jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id),
    FOREIGN KEY (workspace_id, actor_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (actor_agent_id)
);
CREATE INDEX channel_events_workspace_idx ON channel_events(workspace_id, created_at DESC, seq DESC);
CREATE INDEX channel_events_channel_idx ON channel_events(workspace_id, channel_id, created_at DESC, seq DESC);

-- A conversation's origin is a registered type now, not a closed list, and it names the channel it
-- came through (NULL for an origin without one: imported, agent, problem_report). reply_channel_id
-- pins the channel its replies leave from when its own channel cannot send (a web form, an import, a
-- conversation an agent started): set when it starts or on its first reply.
ALTER TABLE conversations DROP CONSTRAINT conversations_channel_check,
    ADD CONSTRAINT conversations_channel_fkey FOREIGN KEY (channel) REFERENCES channel_types(type),
    ADD COLUMN channel_id uuid,
    ADD COLUMN reply_channel_id uuid,
    ADD CONSTRAINT conversations_channel_id_fkey FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id),
    ADD CONSTRAINT conversations_reply_channel_fkey FOREIGN KEY (workspace_id, reply_channel_id) REFERENCES channels(workspace_id, id);
CREATE INDEX conversations_workspace_channel_idx ON conversations(workspace_id, channel_id) WHERE channel_id IS NOT NULL;
CREATE INDEX conversations_workspace_reply_channel_idx ON conversations(workspace_id, reply_channel_id) WHERE reply_channel_id IS NOT NULL;

-- A message's id on its channel ("<type>:<id>", so ids of two types never meet), the channel that
-- carried it, and what that channel knows of it. external_id finds duplicates and threads replies;
-- for email it is "email:" and the Message-ID, moved here from email_message_id below. channel_id is
-- not filled for older messages: which channel carried them cannot be told for sure (a reply on a
-- web form conversation leaves by email).
ALTER TABLE messages
    ADD COLUMN channel_id uuid,
    ADD COLUMN external_id text CHECK (char_length(external_id) BETWEEN 3 AND 1000),
    ADD COLUMN channel_meta jsonb CHECK (channel_meta IS NULL OR (jsonb_typeof(channel_meta) = 'object' AND octet_length(channel_meta::text) <= 16384)),
    ADD CONSTRAINT messages_channel_id_fkey FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id);

-- A customer may be known only on a channel (a chat user id). UNIQUE (workspace_id, email) stays;
-- NULLs never collide.
ALTER TABLE contacts ALTER COLUMN email DROP NOT NULL;

-- The state of a support address domain: its TXT verification and the email provider's domain. It
-- belongs to the domain, not to a mailbox: the email channels of a workspace on one domain verify it
-- once and share one provider domain. A row lives while a channel of the workspace uses the domain.
CREATE TABLE email_domains (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    domain text NOT NULL CHECK (domain = lower(domain) AND domain ~ '^[^@\s]+$'),
    verification_token text NOT NULL,
    verified_at timestamptz,
    provider_domain_id text,
    provider_domain_records jsonb NOT NULL DEFAULT '[]'::jsonb,
    provider_domain_created_at timestamptz,
    provider_domain_expired_at timestamptz,
    provider_domain_first_verified_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, domain)
);
CREATE UNIQUE INDEX email_domains_provider_domain_id_key ON email_domains(provider_domain_id) WHERE provider_domain_id IS NOT NULL;
-- At most one workspace holds a domain name verified through the provider (20261018082052).
CREATE UNIQUE INDEX email_domains_provider_verified_key ON email_domains(domain) WHERE provider_domain_id IS NOT NULL AND verified_at IS NOT NULL;

-- A support address is a plain lowercase addr-spec with one @ and a local part that does not start
-- with +, so the mailbox it names (email.MailboxKey) can be read from it in SQL. Refuse, naming the
-- workspace, rather than change an address a customer forwards to.
DO $$
DECLARE
    bad text;
BEGIN
    SELECT string_agg(format('%s <%s>', w.slug, e.support_address), ', ') INTO bad
        FROM email_channels e JOIN workspaces w ON w.id = e.workspace_id
        WHERE e.support_address !~ '^[^@+\s][^@\s]*@[^@\s]+$';
    IF bad IS NOT NULL THEN
        RAISE EXCEPTION 'support addresses this version cannot keep (more than one @, or a local part that starts with +): %; change them before upgrading', bad;
    END IF;
END
$$;

-- Email and web form settings, the Gmail connection and a consent in flight belong to one channel,
-- which becomes their key below: a workspace may have two support mailboxes or two forms.
-- hosted_local is the local part of the channel's forwarding address on the hosted cloud,
-- <hosted_local>@<slug>.<HOSTED_MAIL_DOMAIN>: support for the channels that exist, so no forwarding
-- set up today changes; a channel added later gets one from its own address.
ALTER TABLE email_channels ADD COLUMN channel_id uuid,
    ADD COLUMN hosted_local text NOT NULL DEFAULT 'support' CHECK (hosted_local ~ '^[a-z0-9][a-z0-9-]{0,62}$');
-- legacy_slug: the form the workspace's older address /api/v1/public/forms/<slug> posts to. Every
-- form has its own address by its id; the older one stays with the form the site embedded.
ALTER TABLE web_form_channels ADD COLUMN channel_id uuid, ADD COLUMN legacy_slug boolean NOT NULL DEFAULT false;
ALTER TABLE mailbox_connections ADD COLUMN channel_id uuid;
ALTER TABLE mailbox_oauth_states ADD COLUMN channel_id uuid;

-- One outbox for every channel; its email rows keep every value.
ALTER TABLE email_outbox RENAME TO channel_outbox;
ALTER TABLE channel_outbox
    ADD COLUMN channel_type text NOT NULL DEFAULT 'email' REFERENCES channel_types(type),
    ADD COLUMN channel_id uuid,
    -- The id the channel gave the sent message.
    ADD COLUMN external_id text CHECK (char_length(external_id) BETWEEN 1 AND 1000),
    -- A code for machines (channel_off, channel_removed...) beside the text of last_error.
    ADD COLUMN last_error_code text CHECK (last_error_code ~ '^[a-z][a-z0-9_]{1,40}$'),
    -- What the channel's adapter keeps about the delivery.
    ADD COLUMN meta jsonb CHECK (meta IS NULL OR (jsonb_typeof(meta) = 'object' AND octet_length(meta::text) <= 16384)),
    ADD CONSTRAINT channel_outbox_channel_fkey FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id),
    -- Only email sends through a transport.
    ALTER COLUMN transport DROP NOT NULL;
ALTER TABLE channel_outbox ALTER COLUMN channel_type DROP DEFAULT;
CREATE INDEX channel_outbox_channel_failed_idx ON channel_outbox(workspace_id, channel_id) WHERE status = 'failed';

-- Backfill. Every email and web form setup becomes a channel, the default of its type, named as the
-- settings page called it. A web form that is off is a channel that is off, since its last change.
INSERT INTO channels (workspace_id, type, name, enabled, is_default, created_at, updated_at)
    SELECT workspace_id, 'email', 'Email', true, true, created_at, updated_at FROM email_channels;
INSERT INTO channels (workspace_id, type, name, enabled, disabled_at, is_default, created_at, updated_at)
    SELECT workspace_id, 'web_form', 'Web form', enabled, CASE WHEN NOT enabled THEN updated_at END, true, created_at, updated_at
    FROM web_form_channels;
-- Email disconnected, or a form switched off for good, before this migration: its conversations and
-- deliveries still name the channel they came through, a removed one.
INSERT INTO channels (workspace_id, type, name, enabled, disabled_at, deleted_at, created_at, updated_at)
    SELECT workspace_id, type, CASE type WHEN 'email' THEN 'Email' ELSE 'Web form' END, false, now(), now(), min(first_at), now()
    FROM (SELECT workspace_id, channel AS type, created_at AS first_at FROM conversations WHERE channel IN ('email', 'web_form')
          UNION ALL SELECT workspace_id, 'email', created_at FROM channel_outbox) used
    WHERE NOT EXISTS (SELECT 1 FROM channels c WHERE c.workspace_id = used.workspace_id AND c.type = used.type)
    GROUP BY workspace_id, type;
INSERT INTO channel_events (workspace_id, channel_id, channel_type, channel_name, kind, detail, created_at)
    SELECT workspace_id, id, type, name, 'added', '{"source": "upgrade"}', created_at FROM channels;
INSERT INTO channel_events (workspace_id, channel_id, channel_type, channel_name, kind, detail, created_at)
    SELECT workspace_id, id, type, name, 'removed', '{"source": "upgrade"}', deleted_at FROM channels WHERE deleted_at IS NOT NULL;

-- The domain state moves to email_domains: one row per workspace now, as one channel had it.
INSERT INTO email_domains (workspace_id, domain, verification_token, verified_at, provider_domain_id, provider_domain_records,
        provider_domain_created_at, provider_domain_expired_at, provider_domain_first_verified_at, created_at, updated_at)
    SELECT workspace_id, split_part(support_address, '@', 2), domain_verification_token, domain_verified_at, provider_domain_id, provider_domain_records,
        provider_domain_created_at, provider_domain_expired_at, provider_domain_first_verified_at, created_at, updated_at
    FROM email_channels;

UPDATE email_channels e SET channel_id = c.id FROM channels c WHERE c.workspace_id = e.workspace_id AND c.type = 'email' AND c.deleted_at IS NULL;
UPDATE web_form_channels f SET channel_id = c.id, legacy_slug = true FROM channels c
    WHERE c.workspace_id = f.workspace_id AND c.type = 'web_form' AND c.deleted_at IS NULL;
UPDATE mailbox_connections m SET channel_id = e.channel_id FROM email_channels e WHERE e.workspace_id = m.workspace_id;
-- A consent of a workspace without email keeps no channel: it cannot finish, and it expires within
-- 10 minutes.
UPDATE mailbox_oauth_states o SET channel_id = e.channel_id FROM email_channels e WHERE e.workspace_id = o.workspace_id;
UPDATE conversations v SET channel_id = c.id FROM channels c WHERE c.workspace_id = v.workspace_id AND c.type = v.channel;
UPDATE channel_outbox o SET channel_id = c.id FROM channels c WHERE c.workspace_id = o.workspace_id AND c.type = 'email';
UPDATE messages SET external_id = 'email:' || email_message_id WHERE email_message_id IS NOT NULL;
INSERT INTO conversation_thread_keys (workspace_id, conversation_id, channel_type, key)
    SELECT workspace_id, id, 'email', 'reply-token:' || email_reply_token FROM conversations WHERE email_reply_token IS NOT NULL;

-- Nothing is left without its channel; a row that would be is a bug of this migration, not something
-- to drop.
DO $$
DECLARE
    missing text;
BEGIN
    SELECT string_agg(what, ', ') INTO missing FROM (
        SELECT 'email_channels' AS what WHERE EXISTS (SELECT 1 FROM email_channels WHERE channel_id IS NULL)
        UNION ALL SELECT 'web_form_channels' WHERE EXISTS (SELECT 1 FROM web_form_channels WHERE channel_id IS NULL)
        UNION ALL SELECT 'mailbox_connections' WHERE EXISTS (SELECT 1 FROM mailbox_connections WHERE channel_id IS NULL)
        UNION ALL SELECT 'mailbox_oauth_states' WHERE EXISTS (SELECT 1 FROM mailbox_oauth_states o WHERE channel_id IS NULL
            AND EXISTS (SELECT 1 FROM email_channels e WHERE e.workspace_id = o.workspace_id))
        UNION ALL SELECT 'channel_outbox' WHERE EXISTS (SELECT 1 FROM channel_outbox WHERE channel_id IS NULL)
        UNION ALL SELECT 'conversations' WHERE EXISTS (SELECT 1 FROM conversations WHERE channel IN ('email', 'web_form') AND channel_id IS NULL)
        UNION ALL SELECT 'messages' WHERE EXISTS (SELECT 1 FROM messages WHERE email_message_id IS NOT NULL AND external_id IS NULL)
    ) left_out;
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'rows without their channel after the backfill: %', missing;
    END IF;
END
$$;

-- The new keys. mailbox_connections pointed at email_channels by workspace; it points by channel now.
ALTER TABLE mailbox_connections DROP CONSTRAINT mailbox_connections_workspace_id_fkey, DROP CONSTRAINT mailbox_connections_pkey;
DROP INDEX email_channels_provider_domain_id_key;
DROP INDEX email_channels_provider_verified_domain_key;
ALTER TABLE email_channels DROP CONSTRAINT email_channels_pkey,
    ALTER COLUMN channel_id SET NOT NULL,
    ADD PRIMARY KEY (channel_id),
    ADD CONSTRAINT email_channels_workspace_channel_key UNIQUE (workspace_id, channel_id),
    ADD CONSTRAINT email_channels_channel_fkey FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id) ON DELETE CASCADE,
    ADD CONSTRAINT email_channels_support_address_shape CHECK (support_address ~ '^[^@+\s][^@\s]*@[^@\s]+$'),
    ALTER COLUMN hosted_local DROP DEFAULT,
    DROP COLUMN domain_verification_token,
    DROP COLUMN domain_verified_at,
    DROP COLUMN provider_domain_id,
    DROP COLUMN provider_domain_records,
    DROP COLUMN provider_domain_created_at,
    DROP COLUMN provider_domain_expired_at,
    DROP COLUMN provider_domain_first_verified_at,
    ADD COLUMN domain text GENERATED ALWAYS AS (split_part(support_address, '@', 2)) STORED,
    -- The mailbox the address names: support+billing@acme.com and support@acme.com are one (email.MailboxKey).
    ADD COLUMN mailbox_key text GENERATED ALWAYS AS (split_part(split_part(support_address, '@', 1), '+', 1) || '@' || split_part(support_address, '@', 2)) STORED;
ALTER TABLE email_channels ADD CONSTRAINT email_channels_domain_fkey FOREIGN KEY (workspace_id, domain) REFERENCES email_domains(workspace_id, domain);
-- Two email channels of a workspace never name one mailbox or share a forwarding address.
CREATE UNIQUE INDEX email_channels_mailbox_idx ON email_channels(workspace_id, mailbox_key);
CREATE UNIQUE INDEX email_channels_hosted_local_idx ON email_channels(workspace_id, hosted_local);

ALTER TABLE web_form_channels DROP CONSTRAINT web_form_channels_pkey,
    ALTER COLUMN channel_id SET NOT NULL,
    ADD PRIMARY KEY (channel_id),
    ADD CONSTRAINT web_form_channels_channel_fkey FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id) ON DELETE CASCADE,
    -- channels.enabled is where a form is on or off.
    DROP COLUMN enabled;
CREATE UNIQUE INDEX web_form_channels_legacy_slug_idx ON web_form_channels(workspace_id) WHERE legacy_slug;

ALTER TABLE mailbox_connections ALTER COLUMN channel_id SET NOT NULL,
    ADD PRIMARY KEY (channel_id),
    ADD CONSTRAINT mailbox_connections_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
    ADD CONSTRAINT mailbox_connections_channel_fkey FOREIGN KEY (workspace_id, channel_id) REFERENCES email_channels(workspace_id, channel_id) ON DELETE CASCADE;
-- One Google account sends for one email channel of a workspace.
CREATE UNIQUE INDEX mailbox_connections_workspace_address_idx ON mailbox_connections(workspace_id, address);
ALTER TABLE mailbox_oauth_states ADD CONSTRAINT mailbox_oauth_states_channel_fkey FOREIGN KEY (workspace_id, channel_id) REFERENCES email_channels(workspace_id, channel_id) ON DELETE CASCADE;

ALTER TABLE channel_outbox ALTER COLUMN channel_id SET NOT NULL;

-- Message-IDs and reply tokens moved to external_id and the thread keys.
DROP INDEX messages_workspace_email_message_id_idx;
ALTER TABLE messages DROP COLUMN email_message_id;
ALTER TABLE conversations DROP COLUMN email_reply_token;

CREATE UNIQUE INDEX messages_workspace_external_id_idx ON messages(workspace_id, external_id) WHERE external_id IS NOT NULL;
CREATE INDEX messages_channel_incoming_recent_idx ON messages(workspace_id, channel_id, created_at) WHERE kind = 'incoming' AND channel_id IS NOT NULL;
-- A reply token finds its conversation on the whole server, as the unique email_reply_token did.
CREATE UNIQUE INDEX conversation_thread_keys_email_token_idx ON conversation_thread_keys(key) WHERE channel_type = 'email';

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
    FOREACH name IN ARRAY ARRAY['channels', 'channel_credentials', 'contact_identities', 'conversation_thread_keys',
            'channel_inbound_events', 'channel_inbound_files', 'channel_sent_ids', 'channel_events', 'email_domains'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', name);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', name);
        EXECUTE format($policy$CREATE POLICY workspace_isolation ON %1$I FOR ALL TO %2$I
            USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
            WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, name, app_role);
        EXECUTE format($policy$CREATE POLICY privileged_access ON %1$I FOR ALL TO %2$I
            USING ((SELECT current_setting('app.privileged', true) = 'on'))
            WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, name, current_user);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON %I TO %I', name, app_role);
    END LOOP;
    -- The default privileges grant every new table to the app role; the registry is not its to read.
    EXECUTE format('REVOKE ALL ON channel_types FROM %I', app_role);
END
$$;
