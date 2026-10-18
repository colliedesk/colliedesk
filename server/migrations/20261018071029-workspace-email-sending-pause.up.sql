-- A workspace whose replies draw many spam complaints stops handing mail to the relay until an operator
-- clears the pause. It is a workspace column, not an email channel column, so disconnecting and
-- reconnecting the channel cannot lift it.
ALTER TABLE workspaces
    ADD COLUMN email_paused_at timestamptz,
    ADD COLUMN email_pause_reason text,
    ADD CONSTRAINT workspaces_email_pause_complete CHECK ((email_paused_at IS NULL) = (email_pause_reason IS NULL));
