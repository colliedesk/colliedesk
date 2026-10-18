-- Older code never deletes provider domains. Ids still waiting here stay in the provider account
-- until someone removes them there.
DROP TABLE IF EXISTS email_provider_domain_trash;
