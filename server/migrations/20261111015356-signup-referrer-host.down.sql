-- Refuse rollback while any sign-up records the site that sent it: dropping the column would lose where that
-- trial came from. Mark the transaction privileged so the refusal also sees rows protected by row-level security.
DO $$
DECLARE
    in_use bigint;
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    SELECT count(*) INTO in_use FROM workspace_onboarding WHERE referrer_host IS NOT NULL;
    IF in_use > 0 THEN
        RAISE EXCEPTION 'cannot roll back: % onboarding row(s) record the site a sign-up came from; preserve it before rollback', in_use;
    END IF;
END
$$;
ALTER TABLE workspace_onboarding DROP COLUMN referrer_host;
