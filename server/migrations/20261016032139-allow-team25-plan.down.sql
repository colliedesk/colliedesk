-- A Team 25 subscription cannot be stored by the older constraint, and deleting or downgrading it
-- would take paid access from a paying workspace (Polar does not redeliver an event it already
-- delivered). So this refuses while any subscription row is on team25: move those workspaces to
-- another plan in Polar first, so their rows are rewritten, then run the down migration again.
-- The row-level security policy shows workspace_billing rows only to a privileged transaction, so
-- the check marks the transaction privileged, as the migration runner does; without it a role
-- that is not a superuser would count 0 rows and the constraint change below would fail instead.
DO $$
DECLARE
    in_use integer;
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    SELECT count(*) INTO in_use FROM workspace_billing WHERE plan = 'team25';
    IF in_use > 0 THEN
        RAISE EXCEPTION 'cannot roll back: % subscription(s) are on the team25 plan; move them to another plan first', in_use;
    END IF;
END
$$;
ALTER TABLE workspace_billing DROP CONSTRAINT workspace_billing_plan_check;
ALTER TABLE workspace_billing ADD CONSTRAINT workspace_billing_plan_check CHECK (plan IN ('team', 'founding'));
