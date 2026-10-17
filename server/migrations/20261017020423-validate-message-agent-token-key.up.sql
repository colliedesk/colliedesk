-- Checks the messages that already exist against the key added by the previous migration. VALIDATE
-- CONSTRAINT needs only a lock that lets reads and writes continue, and it runs in a transaction of its
-- own, which is why it is not a statement of the migration that added the key.
ALTER TABLE messages VALIDATE CONSTRAINT messages_agent_token_fkey;
