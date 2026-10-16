-- Team 25 subscriptions cannot be stored by the older constraint. Their rows only mirror Polar, so
-- they are removed and rebuilt by Polar redelivering the events (or by re-sending subscription.updated
-- from the Polar dashboard); nothing the workspace owns is dropped.
DELETE FROM workspace_billing WHERE plan = 'team25';
ALTER TABLE workspace_billing DROP CONSTRAINT workspace_billing_plan_check;
ALTER TABLE workspace_billing ADD CONSTRAINT workspace_billing_plan_check CHECK (plan IN ('team', 'founding'));
