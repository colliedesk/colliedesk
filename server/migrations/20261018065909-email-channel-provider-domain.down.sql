-- Older code verifies by TXT record only and ignores the provider domain. The provider keeps the domain
-- itself, so dropping our reference loses nothing that cannot be fetched again by creating it.
DROP INDEX IF EXISTS email_channels_provider_domain_id_key;
ALTER TABLE email_channels
    DROP COLUMN IF EXISTS provider_domain_records,
    DROP COLUMN IF EXISTS provider_domain_id;
