-- The Solo (1 agent) and Small (3 agents) plans are sold below Team, so a subscription row may now
-- carry them. Additive: existing rows keep their plan.
ALTER TABLE workspace_billing DROP CONSTRAINT workspace_billing_plan_check;
ALTER TABLE workspace_billing ADD CONSTRAINT workspace_billing_plan_check CHECK (plan IN ('solo', 'small', 'team', 'team25', 'founding'));
