-- The list of provider domains to delete must outlive the workspace that used them: a workspace deleted
-- by retention would otherwise take its ids with it and leave the domains in the provider account for
-- good. The workspace id stays as a record of who held the domain; a row is removed only after the
-- provider confirms the delete, and a failed delete keeps its attempt count and error for the next try.
ALTER TABLE email_provider_domain_trash DROP CONSTRAINT email_provider_domain_trash_workspace_id_fkey;
ALTER TABLE email_provider_domain_trash
    ADD COLUMN attempts integer NOT NULL DEFAULT 0,
    ADD COLUMN last_error text,
    ADD COLUMN next_attempt_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX email_provider_domain_trash_due_idx ON email_provider_domain_trash (next_attempt_at);
