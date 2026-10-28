-- The schema before has no room for a Microsoft connection, the replies sent through it or its failure
-- reason. Refuse to roll back while any exists, naming the workspaces, rather than drop a connection or
-- rewrite how a reply left (the way back for a server is the backup: docs/internals/channels.md).
DO $$
DECLARE
    holders text;
BEGIN
    SELECT string_agg(w.slug, ', ' ORDER BY w.slug) INTO holders FROM workspaces w
        WHERE EXISTS (SELECT 1 FROM mailbox_connections m WHERE m.workspace_id = w.id AND (m.provider = 'microsoft' OR m.error_code = 'mailbox_unavailable'))
            OR EXISTS (SELECT 1 FROM channel_outbox o WHERE o.workspace_id = w.id AND o.transport = 'microsoft');
    IF holders IS NOT NULL THEN
        RAISE EXCEPTION 'Microsoft 365 is connected, or replies went through it, in workspaces %; roll back from a backup instead', holders;
    END IF;
END
$$;

DELETE FROM mailbox_oauth_states WHERE provider = 'microsoft';
ALTER TABLE channel_outbox DROP CONSTRAINT channel_outbox_transport_check,
    ADD CONSTRAINT email_outbox_transport_check CHECK (transport IN ('smtp', 'gmail'));
ALTER TABLE mailbox_connections DROP COLUMN account_id;
ALTER TABLE mailbox_connections DROP CONSTRAINT mailbox_connections_error_code_check,
    ADD CONSTRAINT mailbox_connections_error_code_check CHECK (error_code IN ('revoked', 'permission'));
ALTER TABLE mailbox_oauth_states DROP CONSTRAINT mailbox_oauth_states_provider_check,
    ADD CONSTRAINT mailbox_oauth_states_provider_check CHECK (provider = 'google');
ALTER TABLE mailbox_connections DROP CONSTRAINT mailbox_connections_provider_check,
    ADD CONSTRAINT mailbox_connections_provider_check CHECK (provider = 'google');
