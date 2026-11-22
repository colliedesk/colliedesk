-- A private note can ask teammates by @mention: one row per note and person asked. read_at is when
-- that person opened it (null: unread, their Mentions list counts it). email is the one email that
-- tells them: queued when the note is posted and their agents.mention_email is on, off when it is
-- off, then sending, sent or rejected as the sender settles it (a claim left sending by a stopped
-- server is taken again after a while, from email_claimed_at). Only active agents of the note's
-- workspace are ever asked; the conversation is the note's own, read through messages, so a moved
-- note keeps its mentions.
CREATE TABLE note_mentions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    message_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    read_at timestamptz,
    email text NOT NULL CHECK (email IN ('queued', 'sending', 'sent', 'rejected', 'off')),
    email_claimed_at timestamptz,
    UNIQUE (workspace_id, message_id, agent_id),
    FOREIGN KEY (workspace_id, message_id) REFERENCES messages(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, agent_id) REFERENCES agents(workspace_id, id) ON DELETE CASCADE
);
CREATE INDEX note_mentions_person ON note_mentions(workspace_id, agent_id, created_at DESC);
CREATE INDEX note_mentions_unread ON note_mentions(workspace_id, agent_id) WHERE read_at IS NULL;
CREATE INDEX note_mentions_email ON note_mentions(created_at) WHERE email IN ('queued', 'sending');
ALTER TABLE agents ADD COLUMN mention_email boolean NOT NULL DEFAULT true;

SELECT secure_workspace_table('note_mentions');
