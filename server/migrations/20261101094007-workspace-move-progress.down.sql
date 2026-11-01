-- This only removes guide acknowledgements; imported data, domains and billing are untouched.
ALTER TABLE workspace_onboarding DROP COLUMN move_steps;
