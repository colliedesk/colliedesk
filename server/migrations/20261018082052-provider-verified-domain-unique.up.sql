-- At most one workspace can hold a support domain name verified through the email provider. The provider
-- creates a separate domain, with its own DKIM key, for every request for the same name, so without this
-- two workspaces of one customer could both end up verified, and a complaint about mail from that name
-- could not be attributed to either. Rows verified by the TXT record only are outside the index.
CREATE UNIQUE INDEX email_channels_provider_verified_domain_key ON email_channels ((split_part(support_address, '@', 2)))
    WHERE provider_domain_id IS NOT NULL AND domain_verified_at IS NOT NULL;
