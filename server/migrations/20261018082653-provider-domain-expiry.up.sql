-- When the provider domain was created, so a copy that never verifies can be released after a week and give
-- its slot back; and when that happened, so the settings page can say the records expired.
ALTER TABLE email_channels
    ADD COLUMN provider_domain_created_at timestamptz,
    ADD COLUMN provider_domain_expired_at timestamptz;
UPDATE email_channels SET provider_domain_created_at = updated_at WHERE provider_domain_id IS NOT NULL;
