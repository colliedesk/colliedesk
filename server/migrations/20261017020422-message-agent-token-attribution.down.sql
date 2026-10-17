-- Messages stay; only the record of which token wrote them goes. Dropping the column drops its key.
ALTER TABLE messages DROP COLUMN IF EXISTS agent_token_id;
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_workspace_id_id_key;
