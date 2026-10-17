-- Checks the messages against the key added by the previous migration. VALIDATE CONSTRAINT needs only
-- a lock that lets reads and writes continue, and it runs in a transaction of its own.
ALTER TABLE messages VALIDATE CONSTRAINT messages_agent_request_fkey;
-- Finds the message a request sent (the timeline link and the export). Partial: almost every message has none.
CREATE INDEX IF NOT EXISTS messages_agent_request_idx ON messages(workspace_id, agent_request_id) WHERE agent_request_id IS NOT NULL;
