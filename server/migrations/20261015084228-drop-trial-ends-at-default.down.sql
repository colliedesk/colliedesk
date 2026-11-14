ALTER TABLE workspaces ALTER COLUMN trial_ends_at SET DEFAULT (now() + interval '14 days');
