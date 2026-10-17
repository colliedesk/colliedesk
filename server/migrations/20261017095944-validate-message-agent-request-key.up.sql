-- Checks the messages against the key added by the previous migration. VALIDATE CONSTRAINT needs only
-- a lock that lets reads and writes continue, and it runs in a transaction of its own.
ALTER TABLE messages VALIDATE CONSTRAINT messages_agent_request_fkey;
