-- Rows of workspaces that no longer exist cannot keep the foreign key, so they are dropped; their
-- provider domains then stay in the provider account until someone removes them there.
DROP INDEX IF EXISTS email_provider_domain_trash_due_idx;
ALTER TABLE email_provider_domain_trash
    DROP COLUMN IF EXISTS next_attempt_at,
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS attempts;
DELETE FROM email_provider_domain_trash t WHERE NOT EXISTS (SELECT 1 FROM workspaces w WHERE w.id = t.workspace_id);
ALTER TABLE email_provider_domain_trash
    ADD CONSTRAINT email_provider_domain_trash_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE;
