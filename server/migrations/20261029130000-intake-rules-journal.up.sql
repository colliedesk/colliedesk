-- What a channel's rules decided about each message (intake/rules, docs/internals/channels.md "Rules
-- decide first"): on what basis and by which rule. A match rests on evidence the message carries (a
-- command, a reference, a thread key); a default is what the scope does with a message that carries
-- none, the only decision AI may later replace. Empty for what the frame handled before the rules (a
-- duplicate, an echo, a channel off) and for a message still being collected.
ALTER TABLE channel_inbound_events
    ADD COLUMN decision_basis text CHECK (decision_basis IN ('match', 'default', 'person', 'model')),
    ADD COLUMN decision_rule text CHECK (decision_rule ~ '^[a-z_]{1,40}$'),
    ADD CHECK ((decision_basis IS NULL) = (decision_rule IS NULL));
-- Who sent a message the store finds by its id (a quote in a shared scope, the export's reason for
-- each message) is read by the message; without it each lookup reads the whole journal.
CREATE INDEX channel_inbound_events_message ON channel_inbound_events(workspace_id, message_id) WHERE message_id IS NOT NULL;
