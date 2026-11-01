-- Only confirmations, skips and durable domain milestones need storage. Import and payment
-- completion come from their existing history. The onboarding row already has workspace RLS.
ALTER TABLE workspace_onboarding ADD COLUMN move_steps jsonb NOT NULL DEFAULT '{}'
    CHECK (jsonb_typeof(move_steps) = 'object');
UPDATE workspace_onboarding o SET move_steps =
    CASE WHEN d.verified_at IS NOT NULL THEN '{"custom_domain":"done"}'::jsonb ELSE '{}'::jsonb END ||
    CASE WHEN d.address_checked_at IS NOT NULL AND d.address_error IS NULL THEN '{"cname":"done"}'::jsonb ELSE '{}'::jsonb END
FROM help_center_domains d WHERE d.workspace_id=o.workspace_id AND o.goal='leave_helpdesk';
