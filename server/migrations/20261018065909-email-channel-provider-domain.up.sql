-- The email provider's id for the support address domain, and the DNS records it last listed with their
-- status. The settings page reads the snapshot, so it never waits for the provider.
ALTER TABLE email_channels
    ADD COLUMN provider_domain_id text,
    ADD COLUMN provider_domain_records jsonb NOT NULL DEFAULT '[]'::jsonb;

-- A provider domain belongs to one workspace; a second workspace claiming the same domain id is a bug.
CREATE UNIQUE INDEX email_channels_provider_domain_id_key ON email_channels (provider_domain_id) WHERE provider_domain_id IS NOT NULL;
