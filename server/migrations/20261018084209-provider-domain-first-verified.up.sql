-- When the provider first reported the domain verified. The unverified timeout applies only while this is
-- empty: a domain that was verified once and later reads as pending keeps its provider domain, because the
-- customer has published its DKIM key and releasing it would lose that key.
ALTER TABLE email_channels ADD COLUMN provider_domain_first_verified_at timestamptz;
UPDATE email_channels SET provider_domain_first_verified_at = domain_verified_at WHERE provider_domain_id IS NOT NULL AND domain_verified_at IS NOT NULL;
