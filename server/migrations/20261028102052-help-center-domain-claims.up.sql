-- Each claim of a custom help center domain gets a CNAME target of its own, hc-<claim>.<HELP_CENTER_DOMAIN>:
-- 24 random hex digits made with the claim and kept until the claim is removed or replaced. A CNAME to
-- the workspace's <slug>.<HELP_CENTER_DOMAIN> no longer proves anything: a slug can be renamed and then
-- taken by another workspace, and a CNAME left behind at the domain's DNS provider would prove that
-- workspace's claim. Existing claims get a random target here; their owners point the CNAME at it (the
-- TXT token keeps proving them meanwhile).
ALTER TABLE help_center_domains ADD COLUMN claim text;
UPDATE help_center_domains SET claim = substr(md5(gen_random_uuid()::text || workspace_id::text), 1, 24);
ALTER TABLE help_center_domains ALTER COLUMN claim SET NOT NULL,
    ADD CONSTRAINT help_center_domains_claim_shape CHECK (claim ~ '^[0-9a-f]{24}$');
-- The target names one claim, and a host name below HELP_CENTER_DOMAIN is looked up by it.
CREATE UNIQUE INDEX help_center_domains_claim ON help_center_domains (claim);

-- An active domain whose DNS stops proving the claim goes back to pending after 7 days: failing_since is
-- the first failed check since the last one that passed (null while checks pass, and for a pending
-- domain). pending_reason says why an active domain went back to pending: proof_lost (no proof for 7
-- days) or taken_over (another workspace proved the host name while this one's records did not).
ALTER TABLE help_center_domains ADD COLUMN failing_since timestamptz,
    ADD COLUMN pending_reason text CHECK (pending_reason IN ('proof_lost', 'taken_over')),
    ADD CONSTRAINT help_center_domains_failing_while_active CHECK (failing_since IS NULL OR status = 'active'),
    ADD CONSTRAINT help_center_domains_reason_while_pending CHECK (pending_reason IS NULL OR status = 'pending');
