-- A private note can ask teammates by @mention: one row per note and person asked. read_at is when that
-- person opened it (null: unread, their Mentions list counts it). email is the one email that tells them:
-- queued when the note is posted and their agents.mention_email is on, off when it is off, then sending,
-- sent or rejected as the sender settles it (a claim left sending by a stopped server is taken again after
-- a while, from email_claimed_at). Only active agents of the note's workspace are ever asked; the
-- conversation is the note's own, read through messages, so a moved note keeps its mentions.
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

DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE note_mentions ENABLE ROW LEVEL SECURITY;
    ALTER TABLE note_mentions FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON note_mentions FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON note_mentions FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON note_mentions TO %I', app_role);
END
$$;
