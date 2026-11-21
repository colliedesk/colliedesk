-- Provider domains a workspace no longer uses (its support domain changed or the channel was
-- disconnected) that still have to be deleted at the email provider. The id is written in the same
-- transaction that drops it from the channel, and removed only after the provider confirms the
-- delete, so a failed call is retried on the workspace's next save, verify or disconnect.
CREATE TABLE email_provider_domain_trash (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    provider_domain_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, provider_domain_id)
);

-- Same isolation as the tables of the row-level security migration.
SELECT secure_workspace_table('email_provider_domain_trash');
