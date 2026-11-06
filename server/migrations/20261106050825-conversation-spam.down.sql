-- A conversation in Spam goes back to the status it had before (open for one that arrived in Spam).
UPDATE conversations c SET status = COALESCE((SELECT h.from_status FROM conversation_status_changes h
        WHERE h.workspace_id = c.workspace_id AND h.conversation_id = c.id AND h.to_status = 'spam' AND h.from_status <> 'spam'
        ORDER BY h.changed_at DESC, h.id DESC LIMIT 1), 'open')
    WHERE c.status = 'spam';
ALTER TABLE conversations DROP CONSTRAINT conversations_status_check,
    ADD CONSTRAINT conversations_status_check CHECK (status IN ('open', 'pending', 'resolved', 'closed'));
ALTER TABLE contacts DROP COLUMN spam_since;
