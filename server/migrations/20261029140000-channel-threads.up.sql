-- Where a chat channel's messages are said (docs/internals/channels.md "Chat threads"): a
-- one-to-one chat or a group is a thread, and every message in it is an item, the customer's, a
-- team member's or the channel's own, whether or not it became part of a conversation. A
-- conversation shows the thread around its own messages; the intake reads it for context. The store
-- writes items in the transaction that writes the journal, so an item never exists without its
-- event, and a replay never doubles one.
CREATE TABLE channel_threads (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('direct', 'group')),
    -- The place on the channel, as the type names it ("user:<id>", "group:<id>").
    external_id text NOT NULL CHECK (char_length(external_id) BETWEEN 1 AND 200),
    name text CHECK (char_length(name) <= 200),
    -- A one-to-one chat's customer.
    contact_id uuid,
    -- Whether a message here may open a conversation (a group the channel listens to, a
    -- one-to-one chat): what the alerts of messages nobody concluded on read.
    opens_conversations boolean NOT NULL DEFAULT false,
    last_item_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, channel_id, external_id),
    FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id),
    FOREIGN KEY (workspace_id, contact_id)
        REFERENCES contacts(workspace_id, id) ON DELETE SET NULL (contact_id)
);
CREATE INDEX channel_threads_list
    ON channel_threads(workspace_id, channel_id, last_item_at DESC NULLS LAST, id);

CREATE TABLE channel_thread_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id uuid NOT NULL,
    thread_id uuid NOT NULL,
    direction text NOT NULL CHECK (direction IN ('in', 'out')),
    role text NOT NULL CHECK (role IN ('customer', 'staff', 'bot')),
    sender_kind text CHECK (char_length(sender_kind) <= 40),
    sender_value text CHECK (char_length(sender_value) <= 200),
    sender_name text CHECK (char_length(sender_name) <= 200),
    contact_id uuid,
    agent_id uuid,
    -- The message's id on the channel (without the type), for duplicates and quotes.
    external_id text CHECK (char_length(external_id) BETWEEN 1 AND 1000),
    -- The message it answers, for the context "replying to … ".
    quote_external_id text CHECK (char_length(quote_external_id) BETWEEN 1 AND 1000),
    body text NOT NULL DEFAULT '' CHECK (char_length(body) <= 20000),
    -- An item that came in has its journal event (its files are that event's); one that went out
    -- has its message.
    inbound_event_id uuid,
    message_id uuid,
    -- The batch of intake it belongs to (intake_gatherings, a later migration).
    gathering_id uuid,
    disposition text NOT NULL CHECK (disposition IN (
        'unresolved', 'gathering', 'conversation', 'pending_review', 'not_request', 'staff', 'bot',
        'archived'
    )),
    disposition_reason text CHECK (disposition_reason ~ '^[a-z_]{1,40}$'),
    occurred_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- Each item nobody concluded on is reported once.
    alerted_at timestamptz,
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id),
    FOREIGN KEY (workspace_id, thread_id)
        REFERENCES channel_threads(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, contact_id)
        REFERENCES contacts(workspace_id, id) ON DELETE SET NULL (contact_id),
    FOREIGN KEY (workspace_id, agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (agent_id),
    FOREIGN KEY (workspace_id, inbound_event_id)
        REFERENCES channel_inbound_events(workspace_id, id)
        ON DELETE SET NULL (inbound_event_id),
    FOREIGN KEY (workspace_id, message_id)
        REFERENCES messages(workspace_id, id) ON DELETE SET NULL (message_id),
    -- A customer only writes in; the channel's own messages only go out; a team member's go out
    -- from Colliedesk or come in when typed on the channel's own app.
    CHECK (role <> 'customer' OR direction = 'in'),
    CHECK (role <> 'bot' OR direction = 'out')
);
CREATE UNIQUE INDEX channel_thread_items_external
    ON channel_thread_items(workspace_id, channel_id, external_id) WHERE external_id IS NOT NULL;
CREATE UNIQUE INDEX channel_thread_items_event
    ON channel_thread_items(workspace_id, inbound_event_id) WHERE inbound_event_id IS NOT NULL;
-- A thread's items page by time (the Chat of a conversation, a thread's history).
CREATE INDEX channel_thread_items_thread
    ON channel_thread_items(workspace_id, thread_id, occurred_at DESC, id DESC);
CREATE INDEX channel_thread_items_message
    ON channel_thread_items(workspace_id, message_id) WHERE message_id IS NOT NULL;
CREATE INDEX channel_thread_items_unresolved
    ON channel_thread_items(workspace_id, occurred_at) WHERE disposition = 'unresolved';

-- Which items a conversation holds, with how each came to it; taking one back keeps the row.
CREATE TABLE conversation_thread_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL,
    item_id uuid NOT NULL,
    reason text NOT NULL
        CHECK (reason IN (
            'opened', 'gathered', 'command', 'ai', 'agent', 'reopen', 'reply', 'import'
        )),
    decision_id uuid,
    linked_by_agent_id uuid,
    linked_at timestamptz NOT NULL DEFAULT now(),
    unlinked_at timestamptz,
    unlinked_by_agent_id uuid,
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, conversation_id)
        REFERENCES conversations(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, item_id)
        REFERENCES channel_thread_items(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, linked_by_agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (linked_by_agent_id),
    FOREIGN KEY (workspace_id, unlinked_by_agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (unlinked_by_agent_id)
);
CREATE UNIQUE INDEX conversation_thread_items_live
    ON conversation_thread_items(workspace_id, conversation_id, item_id)
    WHERE unlinked_at IS NULL;
CREATE INDEX conversation_thread_items_conversation
    ON conversation_thread_items(workspace_id, conversation_id, linked_at);
CREATE INDEX conversation_thread_items_item ON conversation_thread_items(workspace_id, item_id);

SELECT secure_workspace_table('channel_threads');
SELECT secure_workspace_table('channel_thread_items');
SELECT secure_workspace_table('conversation_thread_items');
