-- Refuse rollback while any sign-up has attribution: dropping the columns would lose which post or ad
-- brought it. Keep this migration applied until the values are preserved elsewhere. Mark the transaction
-- privileged so the refusal also sees rows protected by row-level security.
DO $$
DECLARE
    in_use bigint;
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    SELECT count(*) INTO in_use FROM workspace_onboarding
        WHERE num_nonnulls(utm_source, utm_medium, utm_campaign, utm_content, utm_term, ttclid, fbclid) > 0;
    IF in_use > 0 THEN
        RAISE EXCEPTION 'cannot roll back: % onboarding row(s) record the post or ad of a sign-up; preserve attribution before rollback', in_use;
    END IF;
END
$$;
ALTER TABLE workspace_onboarding
    DROP CONSTRAINT workspace_onboarding_click_id_seen_at_check,
    DROP COLUMN click_id_seen_at,
    DROP COLUMN fbclid,
    DROP COLUMN ttclid,
    DROP COLUMN utm_term,
    DROP COLUMN utm_content,
    DROP COLUMN utm_campaign,
    DROP COLUMN utm_medium,
    DROP COLUMN utm_source;
