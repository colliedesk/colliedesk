-- The Team 25 plan (25 agents) is sold next to Team and Founding, so a subscription row may now
-- carry it. Additive: existing rows keep their plan.
ALTER TABLE workspace_billing DROP CONSTRAINT workspace_billing_plan_check;
ALTER TABLE workspace_billing ADD CONSTRAINT workspace_billing_plan_check CHECK (plan IN ('team', 'team25', 'founding'));
