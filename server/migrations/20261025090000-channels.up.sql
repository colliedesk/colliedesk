-- Channels (docs/internals/channels.md). A channel is one way customers reach a workspace (its email
-- address, its web form, later a chat account): one row in channels plus the adapter of its type.
-- Everything that arrives goes through one ingest pipeline and every reply leaves through one outbox,
-- whatever the type. Email and the web form keep their own settings tables, now tied to a channel row.
--
-- Older images keep working on this schema after a rollback that keeps it (expand, then contract):
-- the triggers at the end fill the new columns for the writes they make, and email_outbox stays as
-- a view of the renamed outbox. A later migration removes them once no image older than this one
-- can be rolled back to.

-- The registered channel types. connectable=false is only an origin of conversations (an import, a
-- conversation an agent started); nothing connects to it. singleton: at most one live channel of the
-- type per workspace. A new type adds its row in its own migration. Not workspace data: no
-- workspace_id and no grant to the app role (revoked below), which never reads it: the server's
-- registry knows the types, and foreign keys check it as the owner.
CREATE TABLE channel_types (
    type text PRIMARY KEY CHECK (type ~ '^[a-z][a-z0-9_]{1,30}$'),
    connectable boolean NOT NULL,
    singleton boolean NOT NULL DEFAULT false,
    UNIQUE (type, connectable, singleton)
);
INSERT INTO channel_types (type, connectable, singleton) VALUES
    ('email', true, true), ('web_form', true, true),
    ('imported', false, false), ('agent', false, false), ('problem_report', false, false);

CREATE TABLE channels (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    type text NOT NULL,
    -- Copied from channel_types through the foreign key below, so only a connectable type has
    -- channels and the singleton index can read the flag.
    connectable boolean NOT NULL DEFAULT true CHECK (connectable),
    singleton boolean NOT NULL,
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
    UNIQUE (workspace_id, id),
    FOREIGN KEY (type, connectable, singleton) REFERENCES channel_types(type, connectable, singleton),
    FOREIGN KEY (workspace_id, created_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (created_by_agent_id),
    FOREIGN KEY (workspace_id, disabled_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (disabled_by_agent_id),
    FOREIGN KEY (workspace_id, deleted_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (deleted_by_agent_id),
    CHECK (enabled = (disabled_at IS NULL)),
    CHECK (deleted_at IS NULL OR NOT enabled)
);
CREATE UNIQUE INDEX channels_singleton_idx ON channels(workspace_id, type) WHERE singleton AND deleted_at IS NULL;
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
    FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id),
    FOREIGN KEY (workspace_id, conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE SET NULL (conversation_id),
    FOREIGN KEY (workspace_id, message_id) REFERENCES messages(workspace_id, id) ON DELETE SET NULL (message_id),
    FOREIGN KEY (workspace_id, decided_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (decided_by_agent_id),
    CHECK (raw IS NOT NULL OR raw_object_key IS NOT NULL),
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
        'removed', 'held_replayed', 'held_discarded')),
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
-- came through (NULL for an origin without one: imported, agent, problem_report).
ALTER TABLE conversations DROP CONSTRAINT conversations_channel_check,
    ADD CONSTRAINT conversations_channel_fkey FOREIGN KEY (channel) REFERENCES channel_types(type),
    ADD COLUMN channel_id uuid,
    ADD CONSTRAINT conversations_channel_id_fkey FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id);
CREATE INDEX conversations_workspace_channel_idx ON conversations(workspace_id, channel_id) WHERE channel_id IS NOT NULL;

-- A message's id on its channel ("<type>:<id>", so ids of two types never meet), the channel that
-- carried it, and what that channel knows of it. external_id finds duplicates and threads replies;
-- for email it is "email:" and the Message-ID, which stays in email_message_id for older images.
-- channel_id is not filled for older messages: which channel carried them cannot be told for sure (a
-- reply on a web form conversation leaves by email).
ALTER TABLE messages
    ADD COLUMN channel_id uuid,
    ADD COLUMN external_id text CHECK (char_length(external_id) BETWEEN 3 AND 1000),
    ADD COLUMN channel_meta jsonb CHECK (channel_meta IS NULL OR (jsonb_typeof(channel_meta) = 'object' AND octet_length(channel_meta::text) <= 16384)),
    ADD CONSTRAINT messages_channel_id_fkey FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id);

-- A customer may be known only on a channel (a chat user id). UNIQUE (workspace_id, email) stays;
-- NULLs never collide.
ALTER TABLE contacts ALTER COLUMN email DROP NOT NULL;

-- Email and web form settings belong to their channel row. workspace_id stays the key: older images
-- read them by workspace.
ALTER TABLE email_channels ADD COLUMN channel_id uuid UNIQUE,
    ADD CONSTRAINT email_channels_channel_fkey FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id) ON DELETE CASCADE;
ALTER TABLE web_form_channels ADD COLUMN channel_id uuid UNIQUE,
    ADD CONSTRAINT web_form_channels_channel_fkey FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id) ON DELETE CASCADE;

-- One outbox for every channel. Its email rows keep every value; email_outbox becomes a view of them
-- for older images, created below.
ALTER TABLE email_outbox RENAME TO channel_outbox;
ALTER TABLE channel_outbox
    ADD COLUMN channel_type text NOT NULL DEFAULT 'email' REFERENCES channel_types(type),
    -- NULL only for the history of an email channel removed before this migration.
    ADD COLUMN channel_id uuid,
    -- The id the channel gave the sent message.
    ADD COLUMN external_id text CHECK (char_length(external_id) BETWEEN 1 AND 1000),
    -- A code for machines (channel_off, channel_removed...) beside the text of last_error.
    ADD COLUMN last_error_code text CHECK (last_error_code ~ '^[a-z][a-z0-9_]{1,40}$'),
    -- What the channel's adapter keeps about the delivery.
    ADD COLUMN meta jsonb CHECK (meta IS NULL OR (jsonb_typeof(meta) = 'object' AND octet_length(meta::text) <= 16384)),
    ADD CONSTRAINT channel_outbox_channel_fkey FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id),
    -- Only email sends through a transport; the default stays for the inserts of older images.
    ALTER COLUMN transport DROP NOT NULL;
CREATE INDEX channel_outbox_channel_failed_idx ON channel_outbox(workspace_id, channel_id) WHERE status = 'failed';

-- Backfill: every email and web form setup becomes a channel, named as the settings page called it.
-- A web form that is off is a channel that is off, since the time it was last changed.
INSERT INTO channels (workspace_id, type, singleton, name, enabled, created_at, updated_at)
    SELECT workspace_id, 'email', true, 'Email', true, created_at, updated_at FROM email_channels;
INSERT INTO channels (workspace_id, type, singleton, name, enabled, disabled_at, created_at, updated_at)
    SELECT workspace_id, 'web_form', true, 'Web form', enabled, CASE WHEN NOT enabled THEN updated_at END, created_at, updated_at
    FROM web_form_channels;
UPDATE email_channels e SET channel_id = c.id FROM channels c WHERE c.workspace_id = e.workspace_id AND c.type = 'email';
UPDATE web_form_channels f SET channel_id = c.id FROM channels c WHERE c.workspace_id = f.workspace_id AND c.type = 'web_form';
ALTER TABLE email_channels ALTER COLUMN channel_id SET NOT NULL;
ALTER TABLE web_form_channels ALTER COLUMN channel_id SET NOT NULL;
-- So a channel's history starts when it did.
INSERT INTO channel_events (workspace_id, channel_id, channel_type, channel_name, kind, detail, created_at)
    SELECT workspace_id, id, type, name, 'added', '{"source": "upgrade"}', created_at FROM channels;

UPDATE conversations v SET channel_id = c.id FROM channels c WHERE c.workspace_id = v.workspace_id AND c.type = v.channel;
UPDATE messages SET external_id = 'email:' || email_message_id WHERE email_message_id IS NOT NULL;
INSERT INTO conversation_thread_keys (workspace_id, conversation_id, channel_type, key)
    SELECT workspace_id, id, 'email', 'reply-token:' || email_reply_token FROM conversations WHERE email_reply_token IS NOT NULL;
UPDATE channel_outbox o SET channel_id = c.id FROM channels c WHERE c.workspace_id = o.workspace_id AND c.type = 'email';

CREATE UNIQUE INDEX messages_workspace_external_id_idx ON messages(workspace_id, external_id) WHERE external_id IS NOT NULL;
CREATE INDEX messages_channel_incoming_recent_idx ON messages(workspace_id, channel_id, created_at) WHERE kind = 'incoming' AND channel_id IS NOT NULL;

-- The outbox under its old name, for older images: their inserts get channel_type 'email' and the
-- 'smtp' transport from the defaults, and their claim locks only email rows.
CREATE VIEW email_outbox WITH (security_invoker = true) AS
    SELECT id, workspace_id, message_id, status, attempts, next_attempt_at, locked_until, claim_token, last_error, sent_at,
           created_at, updated_at, recipients, held_by_pause, transport
    FROM channel_outbox WHERE channel_type = 'email';

-- Compatibility triggers for older images, which write the old columns only (removed with the view).
-- Each finds the live channel of the type in the workspace; channels are singletons for these types.

-- Connecting email, or saving the web form, creates its channel row when there is none. Both writers
-- upsert, and a BEFORE INSERT trigger runs before the conflict is found, so an existing channel is
-- reused rather than a second one created.
CREATE FUNCTION email_channels_link_channel() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.channel_id IS NULL THEN
        SELECT id INTO NEW.channel_id FROM channels
            WHERE workspace_id = NEW.workspace_id AND type = 'email' AND singleton AND deleted_at IS NULL;
        IF NEW.channel_id IS NULL THEN
            INSERT INTO channels (workspace_id, type, singleton, name) VALUES (NEW.workspace_id, 'email', true, 'Email')
                RETURNING id INTO NEW.channel_id;
            INSERT INTO channel_events (workspace_id, channel_id, channel_type, channel_name, kind)
                VALUES (NEW.workspace_id, NEW.channel_id, 'email', 'Email', 'added');
        END IF;
    END IF;
    RETURN NEW;
END
$$;
CREATE TRIGGER email_channels_link_channel BEFORE INSERT ON email_channels
    FOR EACH ROW EXECUTE FUNCTION email_channels_link_channel();

-- Disconnecting email removes its channel.
CREATE FUNCTION email_channels_remove_channel() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE channels SET enabled = false, disabled_at = COALESCE(disabled_at, now()), deleted_at = now(), updated_at = now()
        WHERE workspace_id = OLD.workspace_id AND id = OLD.channel_id AND deleted_at IS NULL
            -- Not while the workspace itself is being deleted: its channels go with it.
            AND EXISTS (SELECT 1 FROM workspaces w WHERE w.id = OLD.workspace_id);
    IF FOUND THEN
        INSERT INTO channel_events (workspace_id, channel_id, channel_type, channel_name, kind)
            SELECT workspace_id, id, type, name, 'removed' FROM channels WHERE workspace_id = OLD.workspace_id AND id = OLD.channel_id;
    END IF;
    RETURN NULL;
END
$$;
CREATE TRIGGER email_channels_remove_channel AFTER DELETE ON email_channels
    FOR EACH ROW EXECUTE FUNCTION email_channels_remove_channel();

CREATE FUNCTION web_form_channels_link_channel() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.channel_id IS NULL THEN
        SELECT id INTO NEW.channel_id FROM channels
            WHERE workspace_id = NEW.workspace_id AND type = 'web_form' AND singleton AND deleted_at IS NULL;
        IF NEW.channel_id IS NULL THEN
            INSERT INTO channels (workspace_id, type, singleton, name, enabled, disabled_at)
                VALUES (NEW.workspace_id, 'web_form', true, 'Web form', NEW.enabled, CASE WHEN NOT NEW.enabled THEN now() END)
                RETURNING id INTO NEW.channel_id;
            INSERT INTO channel_events (workspace_id, channel_id, channel_type, channel_name, kind)
                VALUES (NEW.workspace_id, NEW.channel_id, 'web_form', 'Web form', 'added');
        END IF;
    END IF;
    RETURN NEW;
END
$$;
CREATE TRIGGER web_form_channels_link_channel BEFORE INSERT ON web_form_channels
    FOR EACH ROW EXECUTE FUNCTION web_form_channels_link_channel();

-- channels.enabled is the truth; an older image turns the form on and off in web_form_channels, and
-- the channel follows. Current code writes both in one transaction, so this finds nothing to do.
CREATE FUNCTION web_form_channels_sync_enabled() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE channels SET enabled = NEW.enabled, disabled_at = CASE WHEN NEW.enabled THEN NULL ELSE now() END, updated_at = now()
        WHERE workspace_id = NEW.workspace_id AND id = NEW.channel_id AND enabled IS DISTINCT FROM NEW.enabled AND deleted_at IS NULL;
    IF FOUND THEN
        INSERT INTO channel_events (workspace_id, channel_id, channel_type, channel_name, kind)
            SELECT workspace_id, id, type, name, CASE WHEN NEW.enabled THEN 'enabled' ELSE 'disabled' END
            FROM channels WHERE workspace_id = NEW.workspace_id AND id = NEW.channel_id;
    END IF;
    RETURN NULL;
END
$$;
CREATE TRIGGER web_form_channels_sync_enabled AFTER INSERT OR UPDATE OF enabled ON web_form_channels
    FOR EACH ROW EXECUTE FUNCTION web_form_channels_sync_enabled();

-- A conversation or a delivery written without its channel gets the live channel of its type.
CREATE FUNCTION conversations_default_channel() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    SELECT id INTO NEW.channel_id FROM channels
        WHERE workspace_id = NEW.workspace_id AND type = NEW.channel AND singleton AND deleted_at IS NULL;
    RETURN NEW;
END
$$;
CREATE TRIGGER conversations_default_channel BEFORE INSERT ON conversations
    FOR EACH ROW WHEN (NEW.channel_id IS NULL) EXECUTE FUNCTION conversations_default_channel();

CREATE FUNCTION channel_outbox_default_channel() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    SELECT id INTO NEW.channel_id FROM channels
        WHERE workspace_id = NEW.workspace_id AND type = NEW.channel_type AND singleton AND deleted_at IS NULL;
    RETURN NEW;
END
$$;
CREATE TRIGGER channel_outbox_default_channel BEFORE INSERT ON channel_outbox
    FOR EACH ROW WHEN (NEW.channel_id IS NULL) EXECUTE FUNCTION channel_outbox_default_channel();

-- A Message-ID written the old way is also the message's external id.
CREATE FUNCTION messages_email_external_id() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.email_message_id IS NOT NULL AND (NEW.external_id IS NULL OR NEW.external_id = 'email:' || OLD.email_message_id) THEN
        NEW.external_id := 'email:' || NEW.email_message_id;
    END IF;
    RETURN NEW;
END
$$;
CREATE TRIGGER messages_email_external_id BEFORE INSERT OR UPDATE OF email_message_id ON messages
    FOR EACH ROW EXECUTE FUNCTION messages_email_external_id();

-- A reply token written the old way is also the conversation's email thread key.
CREATE FUNCTION conversations_email_thread_key() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.email_reply_token IS NOT NULL AND (TG_OP = 'INSERT' OR OLD.email_reply_token IS DISTINCT FROM NEW.email_reply_token) THEN
        INSERT INTO conversation_thread_keys (workspace_id, conversation_id, channel_type, key)
            VALUES (NEW.workspace_id, NEW.id, 'email', 'reply-token:' || NEW.email_reply_token) ON CONFLICT DO NOTHING;
    END IF;
    RETURN NULL;
END
$$;
CREATE TRIGGER conversations_email_thread_key AFTER INSERT OR UPDATE OF email_reply_token ON conversations
    FOR EACH ROW EXECUTE FUNCTION conversations_email_thread_key();

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
            'channel_inbound_events', 'channel_events'] LOOP
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
    -- The view reads channel_outbox as the caller (security_invoker), so its policies still apply.
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON email_outbox TO %I', app_role);
END
$$;
