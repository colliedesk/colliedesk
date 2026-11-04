-- Refuse rollback while any sign-up has a landing page: dropping the column would lose which page brought
-- it. Keep this migration applied until the values are preserved elsewhere. Mark the transaction
-- privileged so the refusal also sees rows protected by row-level security.
DO $$
DECLARE
    in_use bigint;
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    SELECT count(*) INTO in_use FROM workspace_onboarding WHERE landing_path IS NOT NULL;
    IF in_use > 0 THEN
        RAISE EXCEPTION 'cannot roll back: % onboarding row(s) record the landing page of a sign-up; preserve it before rollback', in_use;
    END IF;
END
$$;
ALTER TABLE workspace_onboarding DROP COLUMN landing_path;
