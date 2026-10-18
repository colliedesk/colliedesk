ALTER TABLE email_channels
    DROP COLUMN IF EXISTS provider_domain_expired_at,
    DROP COLUMN IF EXISTS provider_domain_created_at;
