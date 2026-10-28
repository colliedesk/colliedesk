-- An older image serves every help center on its slug address again; the custom domains set since the
-- upgrade are lost, and their visitors reach the app's sign-in page until the DNS records change.
DROP TABLE IF EXISTS help_center_domains;
