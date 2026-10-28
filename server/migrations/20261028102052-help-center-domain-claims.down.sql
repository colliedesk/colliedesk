-- The older image proves a custom domain by a CNAME to the workspace's slug host again; claims keep
-- their host names, tokens and status, and lose their own CNAME targets and release state.
DROP INDEX IF EXISTS help_center_domains_claim;
ALTER TABLE help_center_domains DROP COLUMN IF EXISTS claim, DROP COLUMN IF EXISTS failing_since, DROP COLUMN IF EXISTS pending_reason;
