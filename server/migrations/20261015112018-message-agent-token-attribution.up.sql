-- A message written with an agent token names that token, so the timeline can tell what an agent
-- said from what a person said and an owner can see what a token did before revoking it.
-- NULL for everything a person wrote and for mail that arrived by itself. The link is composite so a
-- message can only name a token of its own workspace. Removing the agent clears the link and keeps
-- the message in its place.
-- The key is added NOT VALID: it checks every new row at once but does not scan the existing messages
-- while writes wait. The next migration validates it.
ALTER TABLE sessions ADD CONSTRAINT sessions_workspace_id_id_key UNIQUE (workspace_id, id);
ALTER TABLE messages ADD COLUMN agent_token_id uuid;
ALTER TABLE messages ADD CONSTRAINT messages_agent_token_fkey
    FOREIGN KEY (workspace_id, agent_token_id) REFERENCES sessions(workspace_id, id) ON DELETE SET NULL (agent_token_id) NOT VALID;
