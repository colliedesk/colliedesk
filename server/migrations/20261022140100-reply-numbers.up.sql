-- Every reply (an outgoing message) gets its number in its conversation, 1, 2, 3...
-- (docs/internals/presence.md, "Warning before sending"). A reply sent with seen_reply_number below the
-- conversation's newest number answers 409: somebody replied since the sender started writing. Numbers,
-- not times: created_at is taken before a transaction commits, so a comparison of times misses a reply
-- that committed late.

ALTER TABLE messages ADD COLUMN reply_number integer CHECK (reply_number > 0);

-- Existing replies are numbered in the order they were written.
UPDATE messages m SET reply_number = n.number
    FROM (SELECT workspace_id, id, row_number() OVER (PARTITION BY workspace_id, conversation_id ORDER BY created_at, id) AS number
          FROM messages WHERE kind = 'outgoing') n
    WHERE m.workspace_id = n.workspace_id AND m.id = n.id;

ALTER TABLE messages ADD CONSTRAINT messages_reply_number_kind CHECK ((kind = 'outgoing') = (reply_number IS NOT NULL));
-- Also how the newest number is read: max(reply_number) of one conversation is one index probe.
CREATE UNIQUE INDEX messages_reply_number_idx ON messages(workspace_id, conversation_id, reply_number) WHERE reply_number IS NOT NULL;

-- Every way a reply is written gets its number here, importers and code older than this migration
-- included. Two writers on one conversation take turns on a transaction-scoped advisory lock named after
-- the conversation (the same key as store.lockReplyNumbers), then read the newest number, which the other
-- has committed by then. The conversation row is not locked here: a writer that locks conversations in
-- its own order (a contact merge, a bulk change) never waits in a cycle with a reply. Within one statement
-- a later row sees the numbers of the rows before it.
CREATE FUNCTION messages_number_reply() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.kind <> 'outgoing' THEN
        NEW.reply_number := NULL;
        RETURN NEW;
    END IF;
    PERFORM pg_advisory_xact_lock(hashtextextended('reply_number:' || NEW.workspace_id::text || ':' || NEW.conversation_id::text, 0));
    SELECT coalesce(max(m.reply_number), 0) + 1 INTO NEW.reply_number FROM messages m
        WHERE m.workspace_id = NEW.workspace_id AND m.conversation_id = NEW.conversation_id AND m.reply_number IS NOT NULL;
    RETURN NEW;
END
$$;
CREATE TRIGGER messages_number_reply BEFORE INSERT ON messages FOR EACH ROW EXECUTE FUNCTION messages_number_reply();
